// Copyright 2021 The Erigon Authors
// This file is part of Erigon.
//
// Erigon is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// Erigon is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with Erigon. If not, see <http://www.gnu.org/licenses/>.

package remotedbserver

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/erigontech/erigon-lib/common"
	"github.com/erigontech/erigon-lib/common/dbg"
	remote "github.com/erigontech/erigon-lib/gointerfaces/remoteproto"
	types "github.com/erigontech/erigon-lib/gointerfaces/typesproto"
	"github.com/erigontech/erigon-lib/kv"
	"github.com/erigontech/erigon-lib/kv/order"
	"github.com/erigontech/erigon-lib/kv/stream"
	"github.com/erigontech/erigon-lib/log/v3"
)

// MaxTxTTL - kv interface provide high-consistancy guaranties: Serializable Isolations Level https://en.wikipedia.org/wiki/Isolation_(database_systems)
// But it comes with cost: DB will start grow if run too long read transactions (hours)
// We decided limit TTL of transaction to `MaxTxTTL`
//
// It means you sill have `Serializable` if tx living < `MaxTxTTL`
// You start have Read Committed Level if tx living > `MaxTxTTL`
//
// It's done by `renew` method: after `renew` call reader will see all changes committed after last `renew` call.
//
// Erigon has much Historical data - which is immutable: reading of historical data for hours still gives you consistant data.
const MaxTxTTL = 60 * time.Second

// KvServiceAPIVersion - use it to track changes in API
// 1.1.0 - added pending transactions, add methods eth_getRawTransactionByHash, eth_retRawTransactionByBlockHashAndIndex, eth_retRawTransactionByBlockNumberAndIndex| Yes     |                                            |
// 1.2.0 - Added separated services for mining and txpool methods
// 2.0.0 - Rename all buckets
// 3.0.0 - ??
// 4.0.0 - Server send tx.ViewID() after open tx
// 5.0 - BlockTransaction table now has canonical ids (txs of non-canonical blocks moving to NonCanonicalTransaction table)
// 5.1.0 - Added blockGasLimit to the StateChangeBatch
// 6.0.0 - Blocks now have system-txs - in the begin/end of block
// 6.1.0 - Add methods Range, IndexRange, HistorySeek, HistoryRange
// 6.2.0 - Add HistoryFiles to reply of Snapshots() method
var KvServiceAPIVersion = &types.VersionReply{Major: 7, Minor: 0, Patch: 0}

type KvServer struct {
	remote.UnimplementedKVServer // must be embedded to have forward compatible implementations.

	kv                 kv.RoDB
	stateChangeStreams *StateChangePubSub
	blockSnapshots     Snapshots
	borSnapshots       Snapshots
	historySnapshots   Snapshots
	ctx                context.Context

	//v3 fields
	txIdGen    atomic.Uint64
	txsMapLock *sync.RWMutex
	txs        map[uint64]*threadSafeTx

	trace     bool
	rangeStep int // make sure `s.with` has limited time
	logger    log.Logger
}

type threadSafeTx struct {
	kv.Tx
	sync.Mutex
}

//go:generate mockgen -typed=true -destination=./snapshots_mock.go -package=remotedbserver . Snapshots
type Snapshots interface {
	Files() []string
}

func NewKvServer(ctx context.Context, db kv.RoDB, snapshots Snapshots, borSnapshots Snapshots, historySnapshots Snapshots, logger log.Logger) *KvServer {
	return &KvServer{
		trace:              false,
		rangeStep:          1024,
		kv:                 db,
		stateChangeStreams: newStateChangeStreams(),
		ctx:                ctx,
		blockSnapshots:     snapshots,
		borSnapshots:       borSnapshots,
		historySnapshots:   historySnapshots,
		txs:                map[uint64]*threadSafeTx{},
		txsMapLock:         &sync.RWMutex{},
		logger:             logger,
	}
}

// Version returns the service-side interface version number
func (s *KvServer) Version(context.Context, *emptypb.Empty) (*types.VersionReply, error) {
	dbSchemaVersion := &kv.DBSchemaVersion
	if KvServiceAPIVersion.Major > dbSchemaVersion.Major {
		return KvServiceAPIVersion, nil
	}
	if dbSchemaVersion.Major > KvServiceAPIVersion.Major {
		return dbSchemaVersion, nil
	}
	if KvServiceAPIVersion.Minor > dbSchemaVersion.Minor {
		return KvServiceAPIVersion, nil
	}
	if dbSchemaVersion.Minor > KvServiceAPIVersion.Minor {
		return dbSchemaVersion, nil
	}
	return dbSchemaVersion, nil
}

func (s *KvServer) begin(ctx context.Context) (id uint64, err error) {
	if s.trace {
		s.logger.Info(fmt.Sprintf("[kv_server] begin %d %s\n", id, dbg.Stack()))
	}
	s.txsMapLock.Lock()
	defer s.txsMapLock.Unlock()
	tx, errBegin := s.kv.BeginRo(ctx) //nolint:gocritic
	if errBegin != nil {
		return 0, errBegin
	}
	id = s.txIdGen.Add(1)
	s.txs[id] = &threadSafeTx{Tx: tx}
	return id, nil
}

// renew - rollback and begin tx without changing it's `id`
func (s *KvServer) renew(ctx context.Context, id uint64) (err error) {
	if s.trace {
		s.logger.Info(fmt.Sprintf("[kv_server] renew %d %s\n", id, dbg.Stack()[:2]))
	}
	s.txsMapLock.Lock()
	defer s.txsMapLock.Unlock()
	tx, ok := s.txs[id]
	if ok {
		tx.Lock()
		defer tx.Unlock()
		tx.Rollback()
	}
	newTx, errBegin := s.kv.BeginRo(ctx) //nolint:gocritic
	if errBegin != nil {
		return fmt.Errorf("kvserver: %w", err)
	}
	s.txs[id] = &threadSafeTx{Tx: newTx}
	return nil
}

func (s *KvServer) rollback(id uint64) {
	if s.trace {
		s.logger.Info(fmt.Sprintf("[kv_server] rollback %d %s\n", id, dbg.Stack()[:2]))
	}
	s.txsMapLock.Lock()
	defer s.txsMapLock.Unlock()
	tx, ok := s.txs[id]
	if ok {
		tx.Lock()
		defer tx.Unlock()
		tx.Rollback() //nolint
		delete(s.txs, id)
	}
}

// with - provides exclusive access to `tx` object. Use it if you need open Cursor or run another method of `tx` object.
// it's ok to use same `kv.RoTx` from different goroutines, but such use must be guarded by `with` method.
//
//	!Important: client may open multiple Cursors and multiple Streams on same `tx` in same time
//	it means server must do limited amount of work inside `with` method (periodically release `tx` for other streams)
//	long-living server-side streams must read limited-portion of data inside `with`, send this portion to
//	client, portion of data it to client, then read next portion in another `with` call.
//	It will allow cooperative access to `tx` object
func (s *KvServer) with(id uint64, f func(kv.Tx) error) error {
	s.txsMapLock.RLock()
	tx, ok := s.txs[id]
	s.txsMapLock.RUnlock()
	if !ok {
		return fmt.Errorf("txn %d already rollback", id)
	}

	if s.trace {
		s.logger.Info(fmt.Sprintf("[kv_server] with %d try lock %s\n", id, dbg.Stack()[:2]))
	}
	tx.Lock()
	if s.trace {
		s.logger.Info(fmt.Sprintf("[kv_server] with %d can lock %s\n", id, dbg.Stack()[:2]))
	}
	defer func() {
		tx.Unlock()
		if s.trace {
			s.logger.Info(fmt.Sprintf("[kv_server] with %d unlock %s\n", id, dbg.Stack()[:2]))
		}
	}()
	return f(tx.Tx)
}

func (s *KvServer) Tx(stream remote.KV_TxServer) error {
	id, errBegin := s.begin(stream.Context())
	if errBegin != nil {
		return fmt.Errorf("server-side error: %w", errBegin)
	}
	defer s.rollback(id)

	var viewID uint64
	if err := s.with(id, func(tx kv.Tx) error {
		viewID = tx.ViewID()
		return nil
	}); err != nil {
		return fmt.Errorf("kvserver: %w", err)
	}
	if err := stream.Send(&remote.Pair{ViewId: viewID, TxId: id}); err != nil {
		return fmt.Errorf("server-side error: %w", err)
	}

	var CursorID uint32
	type CursorInfo struct {
		bucket string
		c      kv.Cursor
		k, v   []byte //fields to save current position of cursor - used when Tx reopen
	}
	cursors := map[uint32]*CursorInfo{}

	txTicker := time.NewTicker(MaxTxTTL)
	defer txTicker.Stop()

	// send all items to client, if k==nil - still send it to client and break loop
	for {
		in, recvErr := stream.Recv()
		if recvErr != nil {
			if errors.Is(recvErr, io.EOF) { // termination
				return nil
			}
			return fmt.Errorf("server-side error: %w", recvErr)
		}

		//TODO: protect against client - which doesn't send any requests
		select {
		default:
		case <-txTicker.C:
			for _, c := range cursors { // save positions of cursor, will restore after Tx reopening
				k, v, err := c.c.Current()
				if err != nil {
					return fmt.Errorf("kvserver: %w", err)
				}
				c.k = common.CopyBytes(k)
				c.v = common.CopyBytes(v)
			}

			if err := s.renew(stream.Context(), id); err != nil {
				return err
			}
			if err := s.with(id, func(tx kv.Tx) error {
				for _, c := range cursors { // restore all cursors position
					var err error
					c.c, err = tx.Cursor(c.bucket) //nolint:gocritic
					if err != nil {
						return err
					}
					switch casted := c.c.(type) {
					case kv.CursorDupSort:
						v, err := casted.SeekBothRange(c.k, c.v)
						if err != nil {
							return fmt.Errorf("server-side error: %w", err)
						}
						if v == nil { // it may happen that key where we stopped disappeared after transaction reopen, then just move to next key
							_, _, err = casted.Next()
							if err != nil {
								return fmt.Errorf("server-side error: %w", err)
							}
						}
					case kv.Cursor:
						if _, _, err := c.c.Seek(c.k); err != nil {
							return fmt.Errorf("server-side error: %w", err)
						}
					}
				}
				return nil
			}); err != nil {
				return err
			}
		}

		var c kv.Cursor
		if in.BucketName == "" {
			cInfo, ok := cursors[in.Cursor]
			if !ok {
				return fmt.Errorf("server-side error: unknown Cursor=%d, Op=%s", in.Cursor, in.Op)
			}
			c = cInfo.c
		}
		switch in.Op {
		case remote.Op_OPEN:
			CursorID++
			var err error
			if err := s.with(id, func(tx kv.Tx) error {
				c, err = tx.Cursor(in.BucketName) //nolint:gocritic
				if err != nil {
					return err
				}
				return nil
			}); err != nil {
				return fmt.Errorf("kvserver: %w", err)
			}
			cursors[CursorID] = &CursorInfo{
				bucket: in.BucketName,
				c:      c,
			}
			if err := stream.Send(&remote.Pair{CursorId: CursorID}); err != nil {
				return fmt.Errorf("kvserver: %w", err)
			}
			continue
		case remote.Op_OPEN_DUP_SORT:
			CursorID++
			var err error
			if err := s.with(id, func(tx kv.Tx) error {
				c, err = tx.CursorDupSort(in.BucketName) //nolint:gocritic
				if err != nil {
					return err
				}
				return nil
			}); err != nil {
				return fmt.Errorf("kvserver: %w", err)
			}
			cursors[CursorID] = &CursorInfo{
				bucket: in.BucketName,
				c:      c,
			}
			if err := stream.Send(&remote.Pair{CursorId: CursorID}); err != nil {
				return fmt.Errorf("server-side error: %w", err)
			}
			continue
		case remote.Op_CLOSE:
			cInfo, ok := cursors[in.Cursor]
			if !ok {
				return fmt.Errorf("server-side error: unknown Cursor=%d, Op=%s", in.Cursor, in.Op)
			}
			cInfo.c.Close()
			delete(cursors, in.Cursor)
			if err := stream.Send(&remote.Pair{}); err != nil {
				return fmt.Errorf("server-side error: %w", err)
			}
			continue
		default:
		}

		if err := handleOp(c, stream, in); err != nil {
			return fmt.Errorf("server-side error: %w", err)
		}
	}
}

func handleOp(c kv.Cursor, stream remote.KV_TxServer, in *remote.Cursor) error {
	var k, v []byte
	var err error
	switch in.Op {
	case remote.Op_FIRST:
		k, v, err = c.First()
	case remote.Op_FIRST_DUP:
		v, err = c.(kv.CursorDupSort).FirstDup()
	case remote.Op_SEEK:
		k, v, err = c.Seek(in.K)
	case remote.Op_SEEK_BOTH:
		v, err = c.(kv.CursorDupSort).SeekBothRange(in.K, in.V)
	case remote.Op_CURRENT:
		k, v, err = c.Current()
	case remote.Op_LAST:
		k, v, err = c.Last()
	case remote.Op_LAST_DUP:
		v, err = c.(kv.CursorDupSort).LastDup()
	case remote.Op_NEXT:
		k, v, err = c.Next()
	case remote.Op_NEXT_DUP:
		k, v, err = c.(kv.CursorDupSort).NextDup()
	case remote.Op_NEXT_NO_DUP:
		k, v, err = c.(kv.CursorDupSort).NextNoDup()
	case remote.Op_PREV:
		k, v, err = c.Prev()
	//case remote.Op_PREV_DUP:
	//	k, v, err = c.(ethdb.CursorDupSort).Prev()
	//	if err != nil {
	//		return err
	//	}
	//case remote.Op_PREV_NO_DUP:
	//	k, v, err = c.Prev()
	//	if err != nil {
	//		return err
	//	}
	case remote.Op_SEEK_EXACT:
		k, v, err = c.SeekExact(in.K)
	case remote.Op_SEEK_BOTH_EXACT:
		k, v, err = c.(kv.CursorDupSort).SeekBothExact(in.K, in.V)
	default:
		return fmt.Errorf("unknown operation: %s", in.Op)
	}
	if err != nil {
		return err
	}

	if err := stream.Send(&remote.Pair{K: k, V: v}); err != nil {
		return err
	}

	return nil
}

func (s *KvServer) StateChanges(_ *remote.StateChangeRequest, server remote.KV_StateChangesServer) error {
	ch, remove := s.stateChangeStreams.Sub()
	defer remove()
	for {
		select {
		case reply := <-ch:
			if err := server.Send(reply); err != nil {
				return err
			}
		case <-s.ctx.Done():
			return nil
		case <-server.Context().Done():
			return nil
		}
	}
}

func (s *KvServer) SendStateChanges(_ context.Context, sc *remote.StateChangeBatch) {
	s.stateChangeStreams.Pub(sc)
}

func (s *KvServer) Snapshots(_ context.Context, _ *remote.SnapshotsRequest) (reply *remote.SnapshotsReply, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("%v, %s", rec, dbg.Stack())
		}
	}()
	if s.blockSnapshots == nil || reflect.ValueOf(s.blockSnapshots).IsNil() { // nolint
		return &remote.SnapshotsReply{BlocksFiles: []string{}, HistoryFiles: []string{}}, nil
	}

	blockFiles := s.blockSnapshots.Files()
	if s.borSnapshots != nil && !reflect.ValueOf(s.borSnapshots).IsNil() { // nolint
		blockFiles = append(blockFiles, s.borSnapshots.Files()...)
	}

	reply = &remote.SnapshotsReply{BlocksFiles: blockFiles}
	if s.historySnapshots != nil && !reflect.ValueOf(s.historySnapshots).IsNil() { // nolint
		reply.HistoryFiles = s.historySnapshots.Files()
	}

	return reply, nil
}

func (s *KvServer) Sequence(_ context.Context, req *remote.SequenceReq) (reply *remote.SequenceReply, err error) {
	reply = &remote.SequenceReply{}
	if err := s.with(req.TxId, func(tx kv.Tx) error {
		ttx, ok := tx.(kv.TemporalTx)
		if !ok {
			return errors.New("server DB doesn't implement kv.Temporal interface")
		}
		reply.Value, err = ttx.ReadSequence(req.Table)
		return nil
	}); err != nil {
		return nil, err
	}
	return reply, nil
}

type StateChangePubSub struct {
	chans map[uint]chan *remote.StateChangeBatch
	id    uint
	mu    sync.RWMutex
}

func newStateChangeStreams() *StateChangePubSub {
	return &StateChangePubSub{}
}

func (s *StateChangePubSub) Sub() (ch chan *remote.StateChangeBatch, remove func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.chans == nil {
		s.chans = make(map[uint]chan *remote.StateChangeBatch)
	}
	s.id++
	id := s.id
	ch = make(chan *remote.StateChangeBatch, 8)
	s.chans[id] = ch
	return ch, func() { s.remove(id) }
}

func (s *StateChangePubSub) Pub(reply *remote.StateChangeBatch) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, ch := range s.chans {
		common.PrioritizedSend(ch, reply)
	}
}

func (s *StateChangePubSub) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.chans)
}

func (s *StateChangePubSub) remove(id uint) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ch, ok := s.chans[id]
	if !ok { // double-unsubscribe support
		return
	}
	close(ch)
	delete(s.chans, id)
}

//
// Temporal methods
//

func (s *KvServer) GetLatest(_ context.Context, req *remote.GetLatestReq) (reply *remote.GetLatestReply, err error) {
	domainName, err := kv.String2Domain(req.Table)
	if err != nil {
		return nil, err
	}
	reply = &remote.GetLatestReply{}
	if err := s.with(req.TxId, func(tx kv.Tx) error {
		ttx, ok := tx.(kv.TemporalTx)
		if !ok {
			return errors.New("server DB doesn't implement kv.Temporal interface")
		}
		if req.Latest {
			reply.V, _, err = ttx.GetLatest(domainName, req.K)
			if err != nil {
				return err
			}
		} else {
			reply.V, reply.Ok, err = ttx.GetAsOf(domainName, req.K, req.Ts)
			if err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return reply, nil
}

func (s *KvServer) HasPrefix(_ context.Context, req *remote.HasPrefixReq) (*remote.HasPrefixReply, error) {
	domain, err := kv.String2Domain(req.Table)
	if err != nil {
		return nil, err
	}

	reply := &remote.HasPrefixReply{}
	err = s.with(req.TxId, func(tx kv.Tx) error {
		ttx, ok := tx.(kv.TemporalTx)
		if !ok {
			return errors.New("server DB doesn't implement kv.Temporal interface")
		}

		reply.FirstKey, reply.FirstVal, reply.HasPrefix, err = ttx.HasPrefix(domain, req.Prefix)
		return err
	})
	if err != nil {
		return nil, err
	}

	return reply, nil
}

func (s *KvServer) HistorySeek(_ context.Context, req *remote.HistorySeekReq) (reply *remote.HistorySeekReply, err error) {
	reply = &remote.HistorySeekReply{}
	if err := s.with(req.TxId, func(tx kv.Tx) error {
		ttx, ok := tx.(kv.TemporalTx)
		if !ok {
			return errors.New("server DB doesn't implement kv.Temporal interface")
		}
		domain, err := kv.String2Domain(req.Table)
		if err != nil {
			return err
		}
		reply.V, reply.Ok, err = ttx.HistorySeek(domain, req.K, req.Ts)
		if err != nil {
			return err
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return reply, nil
}

const PageSizeLimit = 4 * 4096

func (s *KvServer) IndexRange(_ context.Context, req *remote.IndexRangeReq) (*remote.IndexRangeReply, error) {
	reply := &remote.IndexRangeReply{}
	from, limit := int(req.FromTs), int(req.Limit)
	if req.PageToken != "" {
		var pagination remote.IndexPagination
		if err := unmarshalPagination(req.PageToken, &pagination); err != nil {
			return nil, err
		}
		from, limit = int(pagination.NextTimeStamp), int(pagination.Limit)
	}
	if req.PageSize <= 0 || req.PageSize > PageSizeLimit {
		req.PageSize = PageSizeLimit
	}

	if err := s.with(req.TxId, func(tx kv.Tx) error {
		ttx, ok := tx.(kv.TemporalTx)
		if !ok {
			return errors.New("server DB doesn't implement kv.Temporal interface")
		}
		ii, err := kv.String2InvertedIdx(req.Table)
		if err != nil {
			return err
		}
		it, err := ttx.IndexRange(ii, req.K, from, int(req.ToTs), order.By(req.OrderAscend), limit)
		if err != nil {
			return err
		}
		defer it.Close()
		for it.HasNext() {
			v, err := it.Next()
			if err != nil {
				return err
			}
			reply.Timestamps = append(reply.Timestamps, v)
			limit--

			if len(reply.Timestamps) == int(req.PageSize) && it.HasNext() {
				reply.NextPageToken, err = marshalPagination(&remote.IndexPagination{NextTimeStamp: int64(v), Limit: int64(limit)})
				if err != nil {
					return err
				}
				break
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return reply, nil
}

func (s *KvServer) HistoryRange(_ context.Context, req *remote.HistoryRangeReq) (*remote.Pairs, error) {
	reply := &remote.Pairs{}
	fromTs, limit := int(req.FromTs), int(req.Limit)
	if err := s.with(req.TxId, func(tx kv.Tx) error {
		ttx, ok := tx.(kv.TemporalTx)
		if !ok {
			return fmt.Errorf("server DB doesn't implement kv.Temporal interface")
		}
		domain, err := kv.String2Domain(req.Table)
		if err != nil {
			return err
		}
		it, err := ttx.HistoryRange(domain, fromTs, int(req.ToTs), order.By(req.OrderAscend), limit)
		if err != nil {
			return err
		}
		defer it.Close()
		for it.HasNext() {
			k, v, err := it.Next()
			if err != nil {
				return err
			}
			key := common.CopyBytes(k)
			value := common.CopyBytes(v)
			reply.Keys = append(reply.Keys, key)
			reply.Values = append(reply.Values, value)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return reply, nil
}

func (s *KvServer) RangeAsOf(_ context.Context, req *remote.RangeAsOfReq) (*remote.Pairs, error) {
	domainName, err := kv.String2Domain(req.Table)
	if err != nil {
		return nil, err
	}
	reply := &remote.Pairs{}
	fromKey, toKey, limit := req.FromKey, req.ToKey, int(req.Limit)
	if req.PageToken != "" {
		var pagination remote.PairsPagination
		if err := unmarshalPagination(req.PageToken, &pagination); err != nil {
			return nil, err
		}
		fromKey, limit = pagination.NextKey, int(pagination.Limit)
	}
	if req.PageSize <= 0 || req.PageSize > PageSizeLimit {
		req.PageSize = PageSizeLimit
	}

	if err := s.with(req.TxId, func(tx kv.Tx) error {
		ttx, ok := tx.(kv.TemporalTx)
		if !ok {
			return errors.New("server DB doesn't implement kv.Temporal interface")
		}
		it, err := ttx.RangeAsOf(domainName, fromKey, toKey, req.Ts, order.By(req.OrderAscend), limit)
		if err != nil {
			return err
		}
		defer it.Close()
		for it.HasNext() {
			k, v, err := it.Next()
			if err != nil {
				return err
			}
			key := common.CopyBytes(k)
			value := common.CopyBytes(v)
			reply.Keys = append(reply.Keys, key)
			reply.Values = append(reply.Values, value)
			limit--

			if len(reply.Keys) == int(req.PageSize) && it.HasNext() {
				reply.NextPageToken, err = marshalPagination(&remote.PairsPagination{NextKey: k, Limit: int64(limit)})
				if err != nil {
					return err
				}
				break
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return reply, nil
}

func (s *KvServer) Range(_ context.Context, req *remote.RangeReq) (*remote.Pairs, error) {
	from, limit := req.FromPrefix, int(req.Limit)
	if req.PageToken != "" {
		var pagination remote.PairsPagination
		if err := unmarshalPagination(req.PageToken, &pagination); err != nil {
			return nil, err
		}
		from, limit = pagination.NextKey, int(pagination.Limit)
	}
	if req.PageSize <= 0 || req.PageSize > PageSizeLimit {
		req.PageSize = PageSizeLimit
	}

	reply := &remote.Pairs{}
	var err error
	if err = s.with(req.TxId, func(tx kv.Tx) error {
		var it stream.KV
		it, err = tx.Range(req.Table, from, req.ToPrefix, order.FromBool(req.OrderAscend), limit)
		if err != nil {
			return err
		}
		for it.HasNext() {
			k, v, err := it.Next()
			if err != nil {
				return err
			}
			reply.Keys = append(reply.Keys, k)
			reply.Values = append(reply.Values, v)
			limit--
		}
		if len(reply.Keys) == PageSizeLimit && it.HasNext() {
			nextK, _, err := it.Next()
			if err != nil {
				return err
			}
			reply.NextPageToken, err = marshalPagination(&remote.PairsPagination{NextKey: nextK, Limit: int64(limit)})
			if err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return reply, nil
}

func (s *KvServer) HistoryStartFrom(_ context.Context, req *remote.HistoryStartFromReq) (reply *remote.HistoryStartFromReply, err error) {
	reply = &remote.HistoryStartFromReply{}
	if err := s.with(req.TxId, func(tx kv.Tx) error {
		ttx, ok := tx.(kv.TemporalTx)
		if !ok {
			return errors.New("server DB doesn't implement kv.Temporal interface")
		}
		reply.StartFrom = ttx.Debug().HistoryStartFrom(kv.Domain(req.Domain))
		return nil
	}); err != nil {
		return nil, err
	}

	return reply, nil
}

// see: https://cloud.google.com/apis/design/design_patterns
func marshalPagination(m proto.Message) (string, error) {
	pageToken, err := proto.Marshal(m)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(pageToken), nil
}

func unmarshalPagination(pageToken string, m proto.Message) error {
	token, err := base64.StdEncoding.DecodeString(pageToken)
	if err != nil {
		return err
	}
	if err = proto.Unmarshal(token, m); err != nil {
		return err
	}
	return nil
}
