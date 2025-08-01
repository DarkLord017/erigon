// Copyright 2024 The Erigon Authors
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

package headerdownload

import (
	"bytes"
	"compress/gzip"
	"container/heap"
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math/big"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/erigontech/erigon-db/rawdb"
	"github.com/erigontech/erigon-lib/common"
	"github.com/erigontech/erigon-lib/common/dbg"
	"github.com/erigontech/erigon-lib/common/metrics"
	"github.com/erigontech/erigon-lib/config3"
	"github.com/erigontech/erigon-lib/etl"
	"github.com/erigontech/erigon-lib/kv"
	"github.com/erigontech/erigon-lib/kv/dbutils"
	"github.com/erigontech/erigon-lib/log/v3"
	"github.com/erigontech/erigon-lib/rlp"
	"github.com/erigontech/erigon-lib/types"
	"github.com/erigontech/erigon/execution/consensus"
	"github.com/erigontech/erigon/execution/dataflow"
	"github.com/erigontech/erigon/execution/stagedsync/stages"
	"github.com/erigontech/erigon/turbo/services"
)

const POSPandaBanner = `

    ,,,         ,,,                                               ,,,         ,,,
  ;"   ^;     ;'   ",                                           ;"   ^;     ;'   ",
  ;    s$$$$$$$s     ;                                          ;    s$$$$$$$s     ;
  ,  ss$$$$$$$$$$s  ,'  ooooooooo.    .oooooo.   .oooooo..o     ,  ss$$$$$$$$$$s  ,'
  ;s$$$$$$$$$$$$$$$     '888   'Y88. d8P'  'Y8b d8P'    'Y8     ;s$$$$$$$$$$$$$$$
  $$$$$$$$$$$$$$$$$$     888   .d88'888      888Y88bo.          $$$$$$$$$$$$$$$$$$
 $$$$P""Y$$$Y""W$$$$$    888ooo88P' 888      888 '"Y8888o.     $$$$P""Y$$$Y""W$$$$$
 $$$$  p"$$$"q  $$$$$    888        888      888     '"Y88b    $$$$  p"$$$"q  $$$$$
 $$$$  .$$$$$.  $$$$     888        '88b    d88'oo     .d8P    $$$$  .$$$$$.  $$$$
  $$DcaU$$$$$$$$$$      o888o        'Y8bood8P' 8""88888P'      $$DcaU$$$$$$$$$$
    "Y$$$"*"$$$Y"                                                 "Y$$$"*"$$$Y"
        "$b.$$"                                                       "$b.$$"
       .o.                   .   o8o                         .                 .o8
      .888.                .o8   '"'                       .o8                "888
     .8"888.     .ooooo. .o888oooooo oooo    ooo .oooo.  .o888oo .ooooo.  .oooo888
    .8' '888.   d88' '"Y8  888  '888  '88.  .8' 'P  )88b   888  d88' '88bd88' '888
   .88ooo8888.  888        888   888   '88..8'   .oP"888   888  888ooo888888   888
  .8'     '888. 888   .o8  888 . 888    '888'   d8(  888   888 .888    .o888   888
 o88o     o8888o'Y8bod8P'  "888"o888o    '8'    'Y888""8o  "888"'Y8bod8P''Y8bod88P"

`

// Implements sort.Interface so we can sort the incoming header in the message by block height
type HeadersReverseSort []ChainSegmentHeader

func (h HeadersReverseSort) Len() int {
	return len(h)
}

func (h HeadersReverseSort) Less(i, j int) bool {
	// Note - the ordering is the inverse ordering of the block heights
	if h[i].Number != h[j].Number {
		return h[i].Number > h[j].Number
	}
	return bytes.Compare(h[i].Hash[:], h[j].Hash[:]) > 0
}

func (h HeadersReverseSort) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
}

// Implements sort.Interface so we can sort the incoming header in the message by block height
type HeadersSort []ChainSegmentHeader

func (h HeadersSort) Len() int {
	return len(h)
}

func (h HeadersSort) Less(i, j int) bool {
	// Note - the ordering is the inverse ordering of the block heights
	if h[i].Number != h[j].Number {
		return h[i].Number < h[j].Number
	}
	return bytes.Compare(h[i].Hash[:], h[j].Hash[:]) < 0
}

func (h HeadersSort) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
}

// SingleHeaderAsSegment converts message containing 1 header into one singleton chain segment
func (hd *HeaderDownload) SingleHeaderAsSegment(headerRaw []byte, header *types.Header, penalizePoSBlocks bool) ([]ChainSegmentHeader, Penalty, error) {
	hd.lock.RLock()
	defer hd.lock.RUnlock()

	headerHash := types.RawRlpHash(headerRaw)
	if _, bad := hd.badHeaders[headerHash]; bad {
		hd.stats.RejectedBadHeaders++
		dbg.SaveHeapProfileNearOOM(dbg.SaveHeapWithLogger(&hd.logger))
		hd.logger.Warn("[downloader] SingleHeaderAsSegment: Rejected header marked as bad", "hash", headerHash, "height", header.Number.Uint64())
		return nil, BadBlockPenalty, nil
	}
	if penalizePoSBlocks && header.Difficulty.Sign() == 0 {
		return nil, NewBlockGossipAfterMergePenalty, nil
	}
	h := ChainSegmentHeader{
		Header:    header,
		HeaderRaw: headerRaw,
		Hash:      headerHash,
		Number:    header.Number.Uint64(),
	}
	return []ChainSegmentHeader{h}, NoPenalty, nil
}

// ReportBadHeader -
func (hd *HeaderDownload) ReportBadHeader(headerHash common.Hash) {
	hd.lock.Lock()
	defer hd.lock.Unlock()
	hd.badHeaders[headerHash] = struct{}{}
}

func (hd *HeaderDownload) UnlinkHeader(headerHash common.Hash) {
	hd.lock.Lock()
	defer hd.lock.Unlock()
	// Find the link, remove it and all its descendants from all the queues
	if link, ok := hd.links[headerHash]; ok {
		hd.removeUpwards(link)
	}
}

func (hd *HeaderDownload) IsBadHeader(headerHash common.Hash) bool {
	hd.lock.RLock()
	defer hd.lock.RUnlock()
	_, ok := hd.badHeaders[headerHash]
	return ok
}

func (hd *HeaderDownload) ReportBadHeaderPoS(badHeader, lastValidAncestor common.Hash) {
	hd.lock.Lock()
	defer hd.lock.Unlock()
	hd.badPoSHeaders[badHeader] = lastValidAncestor
}
func (hd *HeaderDownload) IsBadHeaderPoS(tipHash common.Hash) (bad bool, lastValidAncestor common.Hash) {
	hd.lock.RLock()
	defer hd.lock.RUnlock()
	lastValidAncestor, bad = hd.badPoSHeaders[tipHash]
	return
}

func (hd *HeaderDownload) removeUpwards(link *Link) {
	if link == nil {
		return
	}
	if link.header != nil {
		if parentLink, ok := hd.links[link.header.ParentHash]; ok {
			parentLink.RemoveChild(link)
		}
	}
	var toRemove = []*Link{link}
	for len(toRemove) > 0 {
		removal := toRemove[len(toRemove)-1]
		toRemove = toRemove[:len(toRemove)-1]
		delete(hd.links, removal.hash)
		hd.moveLinkToQueue(removal, NoQueue)
		for child := removal.fChild; child != nil; child = child.next {
			toRemove = append(toRemove, child)
		}
		removal.ClearChildren()
	}
}

func (hd *HeaderDownload) MarkAllVerified() {
	hd.lock.Lock()
	defer hd.lock.Unlock()
	for _, link := range hd.insertQueue {
		if !link.verified {
			link.linked = true
			link.verified = true
		}
	}
	for hd.linkQueue.Len() > 0 {
		link := hd.linkQueue[0]
		if !link.verified {
			link.linked = true
			link.verified = true
			hd.moveLinkToQueue(link, InsertQueueID)
		}
	}
}

func (hd *HeaderDownload) removeAnchor(anchor *Anchor) {
	// Anchor is removed from the map, and from the priority queue
	delete(hd.anchors, anchor.parentHash)
	hd.anchorTree.Delete(anchor)
}

func (hd *HeaderDownload) pruneLinkQueue() {
	for hd.linkQueue.Len() > hd.linkLimit {
		link := heap.Pop(&hd.linkQueue).(*Link)
		delete(hd.links, link.hash)
		link.ClearChildren()
		if parentLink, ok := hd.links[link.header.ParentHash]; ok {
			parentLink.RemoveChild(link)
		}
		if anchor, ok := hd.anchors[link.header.ParentHash]; ok {
			anchor.RemoveChild(link)
			if anchor.fLink == nil {
				hd.removeAnchor(anchor)
			}
		}
	}
}

func (hd *HeaderDownload) LogAnchorState() {
	hd.lock.RLock()
	defer hd.lock.RUnlock()
	hd.logAnchorState()
}

func (hd *HeaderDownload) Engine() consensus.Engine {
	hd.lock.RLock()
	defer hd.lock.RUnlock()
	return hd.engine
}

func (hd *HeaderDownload) logAnchorState() {
	//nolint:prealloc
	var ss []string
	currentTime := time.Now()
	for anchorParent, anchor := range hd.anchors {
		// Try to figure out end
		var end uint64
		var searchList []*Link
		for child := anchor.fLink; child != nil; child = child.next {
			searchList = append(searchList, child)
		}
		var bs []int
		for len(searchList) > 0 {
			link := searchList[len(searchList)-1]
			if link.blockHeight > end {
				end = link.blockHeight
			}
			searchList = searchList[:len(searchList)-1]
			for child := link.fChild; child != nil; child = child.next {
				searchList = append(searchList, child)
			}
			bs = append(bs, int(link.blockHeight))
		}
		var sbb strings.Builder
		sbb.Grow(len(bs))
		slices.Sort(bs)
		for j, b := range bs {
			if j == 0 {
				sbb.WriteString(strconv.Itoa(b))
			} else if j == len(bs)-1 {
				if bs[j-1]+1 == b {
					// Close interval
					sbb.WriteString(fmt.Sprintf("-%d", b))
				} else {
					// Standalone
					sbb.WriteString(fmt.Sprintf(" %d", b))
				}
			} else {
				if bs[j-1] == b {
					// Skip
				} else if bs[j-1]+1 == b {
					if b+1 == bs[j+1] {
						// Skip
					} else {
						// Close interval
						sbb.WriteString(fmt.Sprintf("-%d", b))
					}
				} else {
					// Open interval or standalone
					sbb.WriteString(fmt.Sprintf(" %d", b))
				}
			}
		}
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("{%8d", anchor.blockHeight))
		sb.WriteString(fmt.Sprintf("-%d links=%d (%s)}", end, len(bs), sbb.String()))
		sb.WriteString(fmt.Sprintf(" => %x", anchorParent))
		sb.WriteString(fmt.Sprintf(", next retry in %v", anchor.nextRetryTime.Sub(currentTime)))
		ss = append(ss, sb.String())
	}
	sort.Strings(ss)
	hd.logger.Debug("[downloader] Queue sizes", "anchors", hd.anchorTree.Len(), "links", hd.linkQueue.Len(), "persisted", hd.persistedLinkQueue.Len())
	for _, s := range ss {
		hd.logger.Debug(s)
	}
}

func (hd *HeaderDownload) RecoverFromDb(db kv.RoDB) error {
	hd.lock.Lock()
	defer hd.lock.Unlock()

	logEvery := time.NewTicker(30 * time.Second)
	defer logEvery.Stop()

	// Drain persistedLinksQueue and remove links
	for hd.persistedLinkQueue.Len() > 0 {
		link := heap.Pop(&hd.persistedLinkQueue).(*Link)
		delete(hd.links, link.hash)
		link.ClearChildren()
	}
	err := db.View(context.Background(), func(tx kv.Tx) error {
		c, err := tx.Cursor(kv.Headers)
		if err != nil {
			return err
		}
		defer c.Close()
		hd.highestInDb, err = stages.GetStageProgress(tx, stages.Headers)
		if err != nil {
			return err
		}
		// Take hd.persistedLinkLimit headers (with the highest heights) as links
		for k, v, err := c.Last(); k != nil && hd.persistedLinkQueue.Len() < hd.persistedLinkLimit; k, v, err = c.Prev() {
			if err != nil {
				return err
			}
			var header types.Header
			if err = rlp.DecodeBytes(v, &header); err != nil {
				return err
			}
			if header.Number.Uint64() <= hd.highestInDb {
				h := ChainSegmentHeader{
					HeaderRaw: v,
					Header:    &header,
					Hash:      types.RawRlpHash(v),
					Number:    header.Number.Uint64(),
				}
				hd.addHeaderAsLink(h, true /* persisted */)
			}

			select {
			case <-logEvery.C:
				hd.logger.Info("[downloader] recover headers from db", "left", hd.persistedLinkLimit-hd.persistedLinkQueue.Len())
			default:
			}
		}

		return nil
	})
	if err != nil {
		return err
	}
	return nil
}

// ReadProgressFromDb updates highestInDb field according to the information
// in the database. It is useful in the situations when transaction was
// aborted and highestInDb became out-of-sync
func (hd *HeaderDownload) ReadProgressFromDb(tx kv.RwTx) (err error) {
	hd.lock.Lock()
	defer hd.lock.Unlock()
	hd.highestInDb, err = stages.GetStageProgress(tx, stages.Headers)
	if err != nil {
		return err
	}
	return nil
}

func (hd *HeaderDownload) invalidateAnchor(anchor *Anchor, reason string) {
	hd.logger.Debug("[downloader] Invalidating anchor", "height", anchor.blockHeight, "hash", anchor.parentHash, "reason", reason)
	hd.removeAnchor(anchor)
	for child := anchor.fLink; child != nil; child, child.next = child.next, nil {
		hd.removeUpwards(child)
	}
}

func (hd *HeaderDownload) RequestMoreHeaders(currentTime time.Time) (*HeaderRequest, []PenaltyItem) {
	hd.lock.Lock()
	defer hd.lock.Unlock()
	var penalties []PenaltyItem
	var req *HeaderRequest

	hd.anchorTree.Ascend(func(anchor *Anchor) bool {
		if anchor.blockHeight == 0 { //has no parent
			return true
		}
		if anchor.nextRetryTime.After(currentTime) {
			// We are not ready to retry this anchor yet
			dataflow.HeaderDownloadStates.AddChange(anchor.blockHeight-1, dataflow.HeaderRetryNotReady)
			return true
		}
		if anchor.timeouts >= 10 {
			// Ancestors of this anchor seem to be unavailable, invalidate and move on
			hd.invalidateAnchor(anchor, "suspected unavailability")
			// Add header invalidate
			dataflow.HeaderDownloadStates.AddChange(anchor.blockHeight-1, dataflow.HeaderInvalidated)
			penalties = append(penalties, PenaltyItem{Penalty: AbandonedAnchorPenalty, PeerID: anchor.peerID})
			return true
		}
		req = &HeaderRequest{
			Anchor:  anchor,
			Hash:    anchor.parentHash,
			Number:  anchor.blockHeight - 1,
			Length:  192,
			Skip:    0,
			Reverse: true,
		}
		// Add header requested
		return false
	})
	return req, penalties
}

func (hd *HeaderDownload) requestMoreHeadersForPOS(currentTime time.Time) (timeout bool, request *HeaderRequest, penalties []PenaltyItem) {
	anchor := hd.posAnchor
	if anchor == nil {
		//dataflow.HeaderDownloadStates.AddChange(anchor.blockHeight-1, dataflow.HeaderEmpty)
		hd.logger.Debug("[downloader] No PoS anchor")
		return
	}

	// Only process the anchors for which the nextRetryTime has already come
	if anchor.nextRetryTime.After(currentTime) {
		return
	}

	// TODO: [pos-downloader-tweaks] - we could reduce this number, or config it
	timeout = anchor.timeouts >= 3
	if timeout {
		hd.logger.Warn("[downloader] Timeout", "requestId", hd.requestId, "peerID", common.Bytes2Hex(anchor.peerID[:]))
		penalties = []PenaltyItem{{Penalty: AbandonedAnchorPenalty, PeerID: anchor.peerID}}
		return
	}

	hd.logger.Trace("[downloader] Request header", "number", anchor.blockHeight-1, "length", 192)

	// Request ancestors
	request = &HeaderRequest{
		Anchor:  anchor,
		Hash:    anchor.parentHash,
		Number:  anchor.blockHeight - 1,
		Length:  192,
		Skip:    0,
		Reverse: true,
	}
	return
}

func (hd *HeaderDownload) UpdateStats(req *HeaderRequest, skeleton bool, peer [64]byte) {
	hd.lock.Lock()
	defer hd.lock.Unlock()
	if skeleton {
		hd.stats.SkeletonRequests++
		dataflow.HeaderDownloadStates.AddChange(req.Number, dataflow.HeaderSkeletonRequested)
		if hd.stats.SkeletonReqMinBlock == 0 || req.Number < hd.stats.SkeletonReqMinBlock {
			hd.stats.SkeletonReqMinBlock = req.Number
		}
		if req.Number+req.Length*req.Skip > hd.stats.SkeletonReqMaxBlock {
			hd.stats.SkeletonReqMaxBlock = req.Number + req.Length*(req.Skip+1)
		}
	} else {
		hd.stats.Requests++
		dataflow.HeaderDownloadStates.AddChange(req.Number, dataflow.HeaderRequested)
		// We know that req is reverse request, with Skip == 0, therefore comparing Number with reqMax
		if req.Number > hd.stats.ReqMaxBlock {
			hd.stats.ReqMaxBlock = req.Number
		}
		if hd.stats.ReqMinBlock == 0 || req.Number < hd.stats.ReqMinBlock+req.Length {
			if req.Number >= req.Length {
				hd.stats.ReqMinBlock = req.Number - req.Length
			}
		}
	}
	//hd.logger.Debug("Header request sent", "req", fmt.Sprintf("%+v", req), "peer", fmt.Sprintf("%x", peer)[:8])
}

func (hd *HeaderDownload) UpdateRetryTime(req *HeaderRequest, currentTime time.Time, timeout time.Duration) {
	hd.lock.Lock()
	defer hd.lock.Unlock()
	req.Anchor.timeouts++
	req.Anchor.nextRetryTime = currentTime.Add(timeout)
}

func (hd *HeaderDownload) RequestSkeleton() *HeaderRequest {
	hd.lock.RLock()
	defer hd.lock.RUnlock()

	var stride uint64
	if hd.initialCycle {
		stride = 192
	}
	var length uint64 = 192
	// Include one header that we have already, to make sure the responses are not empty and do not get penalised when we are at the tip of the chain
	from := hd.highestInDb
	if from <= 1 {
		from = 1
	} else {
		from--
	}

	return &HeaderRequest{Number: from, Length: length, Skip: stride, Reverse: false}
}

func (hd *HeaderDownload) VerifyHeader(header *types.Header) error {
	return hd.engine.VerifyHeader(hd.consensusHeaderReader, header, true /* seal */)
}

type FeedHeaderFunc = func(header *types.Header, headerRaw []byte, hash common.Hash, blockHeight uint64) (td *big.Int, err error)

func (hd *HeaderDownload) InsertHeader(hf FeedHeaderFunc, terminalTotalDifficulty *big.Int, logPrefix string, logChannel <-chan time.Time) (bool, bool, uint64, uint64, error) {
	hd.lock.Lock()
	defer hd.lock.Unlock()
	var returnTd *big.Int
	var lastD *big.Int
	var lastTime uint64
	if hd.insertQueue.Len() > 0 {
		link := hd.insertQueue[0]
		_, bad := hd.badHeaders[link.hash]
		if !bad && !link.persisted {
			_, bad = hd.badHeaders[link.header.ParentHash]
		}
		if bad {
			// If the link or its parent is marked bad, throw it out
			hd.removeUpwards(link)
			dataflow.HeaderDownloadStates.AddChange(link.blockHeight, dataflow.HeaderBad)
			hd.stats.RejectedBadHeaders++
			dbg.SaveHeapProfileNearOOM(dbg.SaveHeapWithLogger(&hd.logger))
			hd.logger.Warn("[downloader] InsertHeader: Rejected header marked as bad", "hash", link.hash, "height", link.blockHeight)
			return true, false, 0, lastTime, nil
		}
		if !link.verified {
			if err := hd.VerifyHeader(link.header); err != nil {
				hd.badPoSHeaders[link.hash] = link.header.ParentHash
				if errors.Is(err, consensus.ErrFutureBlock) {
					// This may become valid later
					hd.logger.Warn("[downloader] Added future link", "hash", link.hash, "height", link.blockHeight, "timestamp", link.header.Time)
					return false, false, 0, lastTime, nil // prevent removal of the link from the hd.linkQueue
				} else {
					hd.logger.Debug("[downloader] Verification failed for header", "hash", link.hash, "height", link.blockHeight, "err", err)
					hd.removeUpwards(link)
					dataflow.HeaderDownloadStates.AddChange(link.blockHeight, dataflow.HeaderEvicted)
					hd.stats.InvalidHeaders++
					return true, false, 0, lastTime, nil
				}
			}
		}
		link.verified = true
		// Make sure long insertions do not appear as a stuck stage 1
		select {
		case <-logChannel:
			hd.logger.Info(fmt.Sprintf("[%s] Inserting headers", logPrefix), "progress", hd.highestInDb, "queue", hd.insertQueue.Len())
		default:
		}

		metrics.UpdateBlockConsumerHeaderDownloadDelay(link.header.Time, link.header.Number.Uint64(), hd.logger)

		td, err := hf(link.header, link.headerRaw, link.hash, link.blockHeight)
		if err != nil {
			return false, false, 0, lastTime, err
		}
		// Some blocks may be marked as non-valid on PoS chain because they were far into the future.
		delete(hd.badPoSHeaders, link.hash)
		if td != nil {
			if hd.seenAnnounces.Pop(link.hash) {
				hd.toAnnounce = append(hd.toAnnounce, Announce{Hash: link.hash, Number: link.blockHeight})
			}
			// Check if transition to proof-of-stake happened and stop forward syncing
			if terminalTotalDifficulty != nil {
				if td.Cmp(terminalTotalDifficulty) >= 0 {
					hd.highestInDb = link.blockHeight
					//hd.logger.Info(POSPandaBanner)
					dataflow.HeaderDownloadStates.AddChange(link.blockHeight, dataflow.HeaderInserted)
					return true, true, 0, lastTime, nil
				}
				returnTd = td
				lastD = link.header.Difficulty
			}
		}

		if link.blockHeight > hd.highestInDb {
			if hd.trace {
				hd.logger.Info("[downloader] Highest in DB change", "number", link.blockHeight, "hash", link.hash)
			}
			hd.highestInDb = link.blockHeight
		}
		lastTime = link.header.Time
		link.persisted = true
		link.header = nil // Drop header reference to free memory, as we won't need it anymore
		link.headerRaw = nil
		hd.moveLinkToQueue(link, PersistedQueueID)
		for child := link.fChild; child != nil; child = child.next {
			if !child.persisted {
				hd.moveLinkToQueue(child, InsertQueueID)
			}
		}
		if link.blockHeight == hd.latestMinedBlockNumber {
			dataflow.HeaderDownloadStates.AddChange(link.blockHeight, dataflow.HeaderInserted)
			return false, true, 0, lastTime, nil
		}
	}
	for hd.persistedLinkQueue.Len() > hd.persistedLinkLimit {
		link := heap.Pop(&hd.persistedLinkQueue).(*Link)
		dataflow.HeaderDownloadStates.AddChange(link.blockHeight, dataflow.HeaderEvicted)
		delete(hd.links, link.hash)
		link.ClearChildren()
	}
	var blocksToTTD uint64
	if terminalTotalDifficulty != nil && returnTd != nil && lastD != nil {
		// Calculate the estimation of when TTD will be hit
		var x big.Int
		x.Sub(terminalTotalDifficulty, returnTd)
		x.Div(&x, lastD)
		if x.IsUint64() {
			blocksToTTD = x.Uint64()
		}
	}

	return hd.insertQueue.Len() > 0 && hd.insertQueue[0].blockHeight <= hd.highestInDb+1, false, blocksToTTD, lastTime, nil
}

// InsertHeaders attempts to insert headers into the database, verifying them first
// It returns true in the first return value if the system is "in sync"
func (hd *HeaderDownload) InsertHeaders(hf FeedHeaderFunc, headerLimit uint, terminalTotalDifficulty *big.Int, logPrefix string, logChannel <-chan time.Time, currentTime uint64) (bool, error) {
	var more = true
	var err error
	var force bool
	var blocksToTTD uint64
	var blockTime uint64

	startHeight := hd.highestInDb

	for more {
		if more, force, blocksToTTD, blockTime, err = hd.InsertHeader(hf, terminalTotalDifficulty, logPrefix, logChannel); err != nil {
			return false, err
		}
		if force {
			return true, nil
		}

		if headerLimit > 0 && hd.highestInDb-startHeight > uint64(headerLimit) {
			break
		}
	}
	if blocksToTTD > 0 {
		hd.logger.Trace("Estimated to reaching TTD", "blocks", blocksToTTD)
	}
	hd.lock.RLock()
	defer hd.lock.RUnlock()
	// Whether the latest inserted block is within a minute of the current time, meaning "almost in sync"
	withinMinute := blockTime+60 >= currentTime
	return hd.highestInDb >= hd.preverifiedHeight && withinMinute, nil
}

func (hd *HeaderDownload) SetHeaderToDownloadPoS(hash common.Hash, height uint64) {
	hd.lock.Lock()
	defer hd.lock.Unlock()

	hd.logger.Debug("[downloader] Set posAnchor", "blockHeight", height+1)
	hd.posAnchor = &Anchor{
		parentHash:  hash,
		blockHeight: height + 1,
	}
}

func (hd *HeaderDownload) ProcessHeadersPOS(csHeaders []ChainSegmentHeader, tx kv.Getter, peerId [64]byte) ([]PenaltyItem, error) {
	if len(csHeaders) == 0 {
		return nil, nil
	}

	from, to := csHeaders[0], csHeaders[len(csHeaders)-1]
	hd.logger.Debug(
		"[downloader] Collecting...",
		"from", from.Number,
		"fromHash", from.Hash,
		"to", to.Number,
		"toHash", to.Hash,
		"len", len(csHeaders),
		"fromPeer", hex.EncodeToString(peerId[:]),
	)

	hd.lock.Lock()
	defer hd.lock.Unlock()
	if hd.posAnchor == nil {
		// May happen if peers are sending unrequested header packets after we've synced
		hd.logger.Debug("[downloader] posAnchor is nil")
		return nil, nil
	}

	// Handle request after closing collectors
	if hd.headersCollector == nil {
		return nil, nil
	}

	for _, sh := range csHeaders {
		header := sh.Header
		headerHash := sh.Hash

		if headerHash != hd.posAnchor.parentHash {
			// Code below prevented syncing from Nethermind nodes who disregarded Reverse parameter to GetBlockHeaders messages
			// With this code commented out, the sync proceeds but very slowly (getting 1 header from the response of 192 headers)
			/*
				if hd.posAnchor.blockHeight != 1 && sh.Number != hd.posAnchor.blockHeight-1 {
					hd.logger.Debug("[downloader] posAnchor", "blockHeight", hd.posAnchor.blockHeight)
					//return nil, nil
				}
			*/

			if hd.posAnchor.blockHeight == header.Number.Uint64()+1 {
				hd.logger.Debug("[downloader] Unexpected header", "hash", headerHash, "expected", hd.posAnchor.parentHash, "peerID", common.Bytes2Hex(peerId[:]))
			}

			// Not penalise because we might have sent request twice
			continue
		}

		headerNumber := header.Number.Uint64()
		if err := hd.headersCollector.Collect(dbutils.HeaderKey(headerNumber, headerHash), sh.HeaderRaw); err != nil {
			return nil, err
		}

		hh, err := hd.headerReader.HeaderByHash(context.Background(), tx, header.ParentHash)
		if err != nil {
			return nil, err
		}
		if hh != nil {
			hd.logger.Debug("[downloader] Synced", "requestId", hd.requestId)
			if headerNumber != hh.Number.Uint64()+1 {
				hd.badPoSHeaders[headerHash] = header.ParentHash
				return nil, fmt.Errorf("invalid PoS segment detected: invalid block number. got %d, expected %d", headerNumber, hh.Number.Uint64()+1)
			}
			hd.posAnchor = nil
			hd.posStatus = Synced
			// Wake up stage loop if it is outside any of the stages
			select {
			case hd.DeliveryNotify <- struct{}{}:
			default:
			}
			return nil, nil
		}

		hd.posAnchor = &Anchor{
			parentHash:  header.ParentHash,
			blockHeight: headerNumber,
			peerID:      peerId,
		}

		if headerNumber <= 1 {
			return nil, errors.New("wrong genesis in PoS sync")
		}
	}
	return nil, nil
}

// GrabAnnounces - returns all available announces and forget them
func (hd *HeaderDownload) GrabAnnounces() []Announce {
	hd.lock.Lock()
	defer hd.lock.Unlock()
	res := hd.toAnnounce
	hd.toAnnounce = []Announce{}
	return res
}

func (hd *HeaderDownload) Progress() uint64 {
	hd.lock.RLock()
	defer hd.lock.RUnlock()
	if hd.posSync && hd.posAnchor != nil {
		return hd.posAnchor.blockHeight - 1
	} else {
		return hd.highestInDb
	}
}

func (hd *HeaderDownload) HasLink(linkHash common.Hash) bool {
	hd.lock.RLock()
	defer hd.lock.RUnlock()
	if _, ok := hd.getLink(linkHash); ok {
		return true
	}
	return false
}

func (hd *HeaderDownload) SourcePeerId(linkHash common.Hash) [64]byte {
	hd.lock.RLock()
	defer hd.lock.RUnlock()
	if link, ok := hd.getLink(linkHash); ok {
		return link.peerId
	}
	return [64]byte{}
}

// SaveExternalAnnounce - does mark hash as seen in external announcement
// only such hashes will broadcast further after
func (hd *HeaderDownload) SaveExternalAnnounce(hash common.Hash) {
	hd.lock.Lock()
	defer hd.lock.Unlock()
	hd.seenAnnounces.Add(hash)
}

func (hd *HeaderDownload) getLink(linkHash common.Hash) (*Link, bool) {
	if link, ok := hd.links[linkHash]; ok {
		return link, true
	}
	return nil, false
}

// addHeaderAsLink wraps header into a link and adds it to either queue of persisted links or queue of non-persisted links
func (hd *HeaderDownload) addHeaderAsLink(h ChainSegmentHeader, persisted bool) *Link {
	link := &Link{
		blockHeight: h.Number,
		hash:        h.Hash,
		header:      h.Header,
		headerRaw:   h.HeaderRaw,
		persisted:   persisted,
	}
	if persisted {
		link.linked = true
		link.verified = true
		link.header = nil // Drop header reference to free memory, as we won't need it anymore
		link.headerRaw = nil
	}
	hd.links[h.Hash] = link
	if persisted {
		hd.moveLinkToQueue(link, PersistedQueueID)
	} else {
		hd.moveLinkToQueue(link, EntryQueueID)
	}
	return link
}

func (hi *HeaderInserter) NewFeedHeaderFunc(db kv.StatelessRwTx, headerReader services.HeaderReader) FeedHeaderFunc {
	return func(header *types.Header, headerRaw []byte, hash common.Hash, blockHeight uint64) (*big.Int, error) {
		return hi.FeedHeaderPoW(db, headerReader, header, headerRaw, hash, blockHeight)
	}
}

// Find the forking point - i.e. the latest header on the canonical chain which is an ancestor of this one
// Most common case - forking point is the height of the parent header
func (hi *HeaderInserter) ForkingPoint(db kv.StatelessRwTx, header, parent *types.Header) (forkingPoint uint64, err error) {
	blockHeight := header.Number.Uint64()
	var ch common.Hash
	if fromCache, ok := hi.canonicalCache.Get(blockHeight - 1); ok {
		ch = fromCache
	} else {
		if ch, ok, err = hi.headerReader.CanonicalHash(context.Background(), db, blockHeight-1); err != nil {
			return 0, fmt.Errorf("reading canonical hash for height %d: %w", blockHeight-1, err)
		}
		if !ok {
			log.Warn("[dbg] HeaderInserter.ForkPoint0", "blockHeight", blockHeight)
		}

	}
	if ch == header.ParentHash {
		forkingPoint = blockHeight - 1
		if forkingPoint == 0 {
			log.Warn("[dbg] HeaderInserter.ForkPoint1", "blockHeight", blockHeight)
		}
	} else {
		// Going further back
		ancestorHash := parent.ParentHash
		ancestorHeight := blockHeight - 2
		// Look in the cache first
		for fromCache, ok := hi.canonicalCache.Get(ancestorHeight); ok; fromCache, ok = hi.canonicalCache.Get(ancestorHeight) {
			ch = fromCache
			if ch == ancestorHash {
				break
			}
			ancestor, err := hi.headerReader.Header(context.Background(), db, ancestorHash, ancestorHeight)
			if err != nil {
				return 0, err
			}
			ancestorHash = ancestor.ParentHash
			ancestorHeight--
		}
		// Now look in the DB
		for {
			ch, _, err := hi.headerReader.CanonicalHash(context.Background(), db, ancestorHeight)
			if err != nil {
				return 0, fmt.Errorf("[%s] reading canonical hash for %d: %w", hi.logPrefix, ancestorHeight, err)
			}
			if ch == ancestorHash {
				break
			}
			ancestor, err := hi.headerReader.Header(context.Background(), db, ancestorHash, ancestorHeight)
			if err != nil {
				return 0, err
			}
			if ancestor == nil {
				return 0, fmt.Errorf("[%s] not found header: %d, %x", hi.logPrefix, ancestorHeight, ancestorHash)
			}
			ancestorHash = ancestor.ParentHash
			ancestorHeight--
		}
		// Loop above terminates when either err != nil (handled already) or ch == ancestorHash, therefore ancestorHeight is our forking point
		forkingPoint = ancestorHeight
		if forkingPoint == 0 {
			log.Warn("[dbg] HeaderInserter.ForkPoint2", "blockHeight", blockHeight)
		}
	}
	return
}

func (hi *HeaderInserter) FeedHeaderPoW(db kv.StatelessRwTx, headerReader services.HeaderReader, header *types.Header, headerRaw []byte, hash common.Hash, blockHeight uint64) (td *big.Int, err error) {
	if hash == hi.prevHash {
		// Skip duplicates
		return nil, nil
	}
	oldH, err := headerReader.Header(context.Background(), db, hash, blockHeight)
	if err != nil {
		return nil, err
	}
	if oldH != nil {
		// Already inserted, skip
		return nil, nil
	}
	// Load parent header
	parent, err := headerReader.Header(context.Background(), db, header.ParentHash, blockHeight-1)
	if err != nil {
		return nil, err
	}
	if parent == nil {
		// Fail on headers without parent
		return nil, fmt.Errorf("could not find parent with hash %x and height %d for header %x %d", header.ParentHash, blockHeight-1, hash, blockHeight)
	}
	// Parent's total difficulty
	parentTd, err := rawdb.ReadTd(db, header.ParentHash, blockHeight-1)
	if err != nil || parentTd == nil {
		return nil, fmt.Errorf("[%s] parent's total difficulty not found with hash %x and height %d for header %x %d: %v", hi.logPrefix, header.ParentHash, blockHeight-1, hash, blockHeight, err)
	}
	// Calculate total difficulty of this header using parent's total difficulty
	td = new(big.Int).Add(parentTd, header.Difficulty)

	// Now we can decide whether this header will create a change in the canonical head
	if td.Cmp(hi.localTd) >= 0 {
		reorg := true

		// TODO: Add bor check here if required
		// Borrowed from https://github.com/maticnetwork/bor/blob/master/core/forkchoice.go#L81
		if td.Cmp(hi.localTd) == 0 {
			if blockHeight > hi.highest {
				reorg = false
			} else if blockHeight == hi.highest {
				// Compare hashes of block in case of tie breaker. Lexicographically larger hash wins.
				reorg = bytes.Compare(hi.highestHash.Bytes(), hash.Bytes()) < 0
			}
		}

		if reorg {
			hi.newCanonical = true
			forkingPoint, err := hi.ForkingPoint(db, header, parent)
			if err != nil {
				return nil, err
			}
			hi.highest = blockHeight
			hi.highestHash = hash
			hi.highestTimestamp = header.Time
			hi.canonicalCache.Add(blockHeight, hash)
			// See if the forking point affects the unwindPoint (the block number to which other stages will need to unwind before the new canonical chain is applied)
			if forkingPoint < hi.unwindPoint {
				hi.SetUnwindPoint(forkingPoint)
				hi.unwind = true
			}
			// This makes sure we end up choosing the chain with the max total difficulty
			hi.localTd.Set(td)
		}
	}
	if err = rawdb.WriteTd(db, hash, blockHeight, td); err != nil {
		return nil, fmt.Errorf("[%s] failed to WriteTd: %w", hi.logPrefix, err)
	}
	// skipIndexing=true - because next stages will build indices in-batch (for example StageBlockHash)
	if err = rawdb.WriteHeaderRaw(db, blockHeight, hash, headerRaw, true); err != nil {
		return nil, fmt.Errorf("[%s] failed to WriteTd: %w", hi.logPrefix, err)
	}

	hi.prevHash = hash
	return td, nil
}

func (hi *HeaderInserter) FeedHeaderPoS(db kv.RwTx, header *types.Header, hash common.Hash) error {
	blockHeight := header.Number.Uint64()
	// TODO(yperbasis): do we need to check if the header is already inserted (oldH)?

	parentTd, err := rawdb.ReadTd(db, header.ParentHash, blockHeight-1)
	if err != nil || parentTd == nil {
		return fmt.Errorf("[%s] parent's total difficulty not found with hash %x and height %d for header %x %d: %v", hi.logPrefix, header.ParentHash, blockHeight-1, hash, blockHeight, err)
	}
	td := new(big.Int).Add(parentTd, header.Difficulty)
	if err = rawdb.WriteHeader(db, header); err != nil {
		return fmt.Errorf("[%s] failed to WriteHeader: %w", hi.logPrefix, err)
	}
	if err = rawdb.WriteTd(db, hash, blockHeight, td); err != nil {
		return fmt.Errorf("[%s] failed to WriteTd: %w", hi.logPrefix, err)
	}

	hi.highest = blockHeight
	hi.highestHash = hash
	hi.highestTimestamp = header.Time

	return nil
}

func (hi *HeaderInserter) GetLocalTd() *big.Int {
	return hi.localTd
}

func (hi *HeaderInserter) GetHighest() uint64 {
	return hi.highest
}

func (hi *HeaderInserter) GetHighestHash() common.Hash {
	return hi.highestHash
}

func (hi *HeaderInserter) GetHighestTimestamp() uint64 {
	return hi.highestTimestamp
}

func (hi *HeaderInserter) UnwindPoint() uint64 {
	return hi.unwindPoint
}

func (hi *HeaderInserter) SetUnwindPoint(v uint64) {
	log.Warn("[dbg] HeaderInserter: set unwind point", "v", v, "stack", dbg.Stack())
	hi.unwindPoint = v
}

func (hi *HeaderInserter) Unwind() bool {
	return hi.unwind
}

func (hi *HeaderInserter) BestHeaderChanged() bool {
	return hi.newCanonical
}

func (hd *HeaderDownload) ProcessHeader(sh ChainSegmentHeader, newBlock bool, peerID [64]byte) bool {
	hd.lock.Lock()
	defer hd.lock.Unlock()
	if sh.Number > hd.stats.RespMaxBlock {
		hd.stats.RespMaxBlock = sh.Number
	}
	if hd.stats.RespMinBlock == 0 || sh.Number < hd.stats.RespMinBlock {
		hd.stats.RespMinBlock = sh.Number
	}
	if _, ok := hd.links[sh.Hash]; ok {
		hd.stats.Duplicates++
		// Duplicate
		return false
	}
	parent, foundParent := hd.links[sh.Header.ParentHash]
	anchor, foundAnchor := hd.anchors[sh.Hash]
	if !foundParent && !foundAnchor {
		if sh.Number < hd.highestInDb {
			hd.logger.Debug(fmt.Sprintf("[downloader] new anchor too far in the past: %d, latest header in db: %d", sh.Number, hd.highestInDb))
			return false
		}
		if len(hd.anchors) >= hd.anchorLimit {
			hd.logger.Debug(fmt.Sprintf("[downloader] too many anchors: %d, limit %d", len(hd.anchors), hd.anchorLimit))
			return false
		}
	}
	link := hd.addHeaderAsLink(sh, false /* persisted */)
	if foundAnchor {
		// The new link is what anchor was pointing to, so the link takes over the child links of the anchor and the anchor is removed
		link.fChild = anchor.fLink
		hd.removeAnchor(anchor)
	}
	if parentAnchor, ok := hd.anchors[sh.Header.ParentHash]; ok {
		// Alternative branch connected to an existing anchor
		// Adding link as another child to the anchor and quit (not to overwrite the anchor)
		link.next = parentAnchor.fLink
		parentAnchor.fLink = link
		return false
	}
	if foundParent {
		// Add this link as another child to the parent that is found
		link.next = parent.fChild
		parent.fChild = link
		if parent.persisted {
			link.linked = true
			hd.moveLinkToQueue(link, InsertQueueID)
		}
	} else {
		// The link has not known parent, therefore it becomes an anchor, unless it is too far in the past
		if sh.Number+config3.FullImmutabilityThreshold < hd.highestInDb {
			hd.logger.Debug("[downloader] Remove upwards", "height", link.blockHeight, "hash", link.blockHeight)
			hd.removeUpwards(link)
			return false
		}
		anchor = &Anchor{
			parentHash:    sh.Header.ParentHash,
			nextRetryTime: time.Time{}, // Will ensure this anchor will be top priority
			peerID:        peerID,
			blockHeight:   sh.Number,
		}
		anchor.fLink = link
		hd.anchors[anchor.parentHash] = anchor
		hd.anchorTree.ReplaceOrInsert(anchor)
		return true
	}
	return false
}

func (hd *HeaderDownload) ProcessHeaders(csHeaders []ChainSegmentHeader, newBlock bool, peerID [64]byte) bool {
	requestMore := false
	for _, sh := range csHeaders {
		// Lock is acquired for every invocation of ProcessHeader
		if hd.ProcessHeader(sh, newBlock, peerID) {
			requestMore = true
		}
	}
	hd.lock.Lock()
	defer hd.lock.Unlock()
	hd.stats.Responses++
	hd.logger.Trace("[downloader] Link queue", "size", hd.linkQueue.Len())
	if hd.linkQueue.Len() > hd.linkLimit {
		hd.logger.Trace("[downloader] Too many links, cutting down", "count", hd.linkQueue.Len(), "tried to add", len(csHeaders), "limit", hd.linkLimit)
		hd.pruneLinkQueue()
	}
	// Wake up stage loop if it is outside any of the stages
	select {
	case hd.DeliveryNotify <- struct{}{}:
	default:
	}

	return !hd.initialCycle && requestMore
}

func (hd *HeaderDownload) ExtractStats() Stats {
	hd.lock.RLock()
	defer hd.lock.RUnlock()
	s := hd.stats
	hd.stats = Stats{}
	return s
}

func (hd *HeaderDownload) FirstPoSHeight() *uint64 {
	hd.lock.RLock()
	defer hd.lock.RUnlock()
	return hd.firstSeenHeightPoS
}

func (hd *HeaderDownload) SetFirstPoSHeight(blockHeight uint64) {
	hd.lock.RLock()
	defer hd.lock.RUnlock()
	if hd.firstSeenHeightPoS == nil {
		hd.firstSeenHeightPoS = new(uint64)
		*hd.firstSeenHeightPoS = blockHeight
	}
}

func (hd *HeaderDownload) SetHeaderReader(headerReader consensus.ChainHeaderReader) {
	hd.lock.Lock()
	defer hd.lock.Unlock()
	hd.consensusHeaderReader = headerReader
}

func (hd *HeaderDownload) AfterInitialCycle() {
	hd.lock.Lock()
	defer hd.lock.Unlock()
	hd.initialCycle = false
}

func (hd *HeaderDownload) SetFetchingNew(fetching bool) {
	hd.lock.Lock()
	defer hd.lock.Unlock()
	hd.fetchingNew = fetching
}

func (hd *HeaderDownload) SetPosStatus(status SyncStatus) {
	hd.lock.Lock()
	defer hd.lock.Unlock()
	hd.posStatus = status
}

func (hd *HeaderDownload) HeadersCollector() *etl.Collector {
	hd.lock.RLock()
	defer hd.lock.RUnlock()
	return hd.headersCollector
}

func (hd *HeaderDownload) SetHeadersCollector(collector *etl.Collector) {
	hd.lock.Lock()
	defer hd.lock.Unlock()
	hd.headersCollector = collector
}

func (hd *HeaderDownload) SetPOSSync(posSync bool) {
	hd.lock.Lock()
	defer hd.lock.Unlock()
	hd.posSync = posSync
}

func (hd *HeaderDownload) POSSync() bool {
	hd.lock.RLock()
	defer hd.lock.RUnlock()
	return hd.posSync
}

func (hd *HeaderDownload) PosStatus() SyncStatus {
	hd.lock.RLock()
	defer hd.lock.RUnlock()
	return hd.posStatus
}

func (hd *HeaderDownload) InitialCycle() bool {
	hd.lock.RLock()
	defer hd.lock.RUnlock()
	return hd.initialCycle
}

func (hd *HeaderDownload) FetchingNew() bool {
	hd.lock.RLock()
	defer hd.lock.RUnlock()
	return hd.fetchingNew
}

func (hd *HeaderDownload) GetPendingPayloadHash() common.Hash {
	hd.lock.RLock()
	defer hd.lock.RUnlock()
	return hd.pendingPayloadHash
}

func (hd *HeaderDownload) SetPendingPayloadHash(header common.Hash) {
	hd.lock.Lock()
	defer hd.lock.Unlock()
	hd.pendingPayloadHash = header
}

func (hd *HeaderDownload) ClearPendingPayloadHash() {
	hd.lock.Lock()
	defer hd.lock.Unlock()
	hd.pendingPayloadHash = common.Hash{}
}

func (hd *HeaderDownload) RequestId() int {
	hd.lock.RLock()
	defer hd.lock.RUnlock()
	return hd.requestId
}

func (hd *HeaderDownload) SetRequestId(requestId int) {
	hd.lock.Lock()
	defer hd.lock.Unlock()
	hd.requestId = requestId
}

func (hd *HeaderDownload) AddMinedHeader(header *types.Header) error {
	buf := bytes.NewBuffer(nil)
	if err := header.EncodeRLP(buf); err != nil {
		return err
	}
	segments, _, err := hd.SingleHeaderAsSegment(buf.Bytes(), header, false /* penalizePoSBlocks */)
	if err != nil {
		return err
	}

	peerID := [64]byte{'m', 'i', 'n', 'e', 'r'} // "miner"

	_ = hd.ProcessHeaders(segments, false /* newBlock */, peerID)
	hd.setLatestMinedBlockNumber(header.Number.Uint64())
	return nil
}

func (hd *HeaderDownload) setLatestMinedBlockNumber(num uint64) {
	hd.lock.Lock()
	hd.latestMinedBlockNumber = num
	hd.lock.Unlock()
}

func (hd *HeaderDownload) AddHeadersFromSnapshot(tx kv.Tx, r services.FullBlockReader) error {
	n := r.FrozenBlocks()

	hd.lock.Lock()
	defer hd.lock.Unlock()
	for i := n; i > 0 && hd.persistedLinkQueue.Len() < hd.persistedLinkLimit; i-- {
		header, err := r.HeaderByNumber(context.Background(), tx, i)
		if err != nil {
			return err
		}
		if header == nil {
			continue
		}
		v, err := rlp.EncodeToBytes(header)
		if err != nil {
			return err
		}
		h := ChainSegmentHeader{
			HeaderRaw: v,
			Header:    header,
			Hash:      header.Hash(),
			Number:    header.Number.Uint64(),
		}
		link := hd.addHeaderAsLink(h, true /* persisted */)
		link.verified = true
	}
	if hd.highestInDb < n {
		hd.highestInDb = n
	}
	if hd.preverifiedHeight < n {
		hd.preverifiedHeight = n
	}

	return nil
}

const (
	logInterval = 30 * time.Second
)

func (hd *HeaderDownload) cleanUpPoSDownload() {
	if hd.headersCollector != nil {
		hd.headersCollector.Close()
		hd.headersCollector = nil
	}
	hd.posStatus = Idle
}

func (hd *HeaderDownload) StartPoSDownloader(
	ctx context.Context,
	headerReqSend func(context.Context, *HeaderRequest) ([64]byte, bool),
	penalize func(context.Context, []PenaltyItem),
) {
	go func() {
		prevProgress := uint64(0)

		logEvery := time.NewTicker(logInterval)
		defer logEvery.Stop()

		for {
			var req *HeaderRequest
			var penalties []PenaltyItem
			var currentTime time.Time

			hd.lock.Lock()
			if hd.posStatus == Syncing {
				currentTime = time.Now()
				var timeout bool
				timeout, req, penalties = hd.requestMoreHeadersForPOS(currentTime)
				if timeout {
					hd.cleanUpPoSDownload()
				}
			} else {
				prevProgress = 0
			}
			hd.lock.Unlock()

			if req != nil {
				_, sentToPeer := headerReqSend(ctx, req)
				if sentToPeer {
					// If request was actually sent to a peer, we update retry time to be 5 seconds in the future
					hd.UpdateRetryTime(req, currentTime, 30*time.Second /* timeout */)
					hd.logger.Debug("[downloader] Sent request", "height", req.Number)
				}
			}
			if len(penalties) > 0 {
				penalize(ctx, penalties)
			}

			// Sleep and check for logs
			timer := time.NewTimer(2 * time.Millisecond)
			select {
			case <-ctx.Done():
				hd.lock.Lock()
				hd.cleanUpPoSDownload()
				hd.lock.Unlock()
				return
			case <-logEvery.C:
				if hd.PosStatus() == Syncing {
					progress := hd.Progress()
					if prevProgress == 0 {
						prevProgress = progress
					} else if progress <= prevProgress {
						diff := prevProgress - progress
						hd.logger.Info("[downloader] Downloaded PoS Headers", "now", progress,
							"blk/sec", float64(diff)/float64(logInterval/time.Second))
						prevProgress = progress
					}
				}
			case <-timer.C:
			}
			// Cleanup timer
			timer.Stop()
		}
	}()
}

func DecodeTips(encodings []string) (map[common.Hash]HeaderRecord, error) {
	hardTips := make(map[common.Hash]HeaderRecord, len(encodings))

	var buf bytes.Buffer

	for i, encoding := range encodings {
		b, err := base64.RawStdEncoding.DecodeString(encoding)
		if err != nil {
			return nil, fmt.Errorf("decoding hard coded header on %d: %w", i, err)
		}

		if _, err = buf.Write(b); err != nil {
			return nil, fmt.Errorf("gzip write string on %d: %w", i, err)
		}

		zr, err := gzip.NewReader(&buf)
		if err != nil {
			return nil, fmt.Errorf("gzip reader on %d: %w %q", i, err, encoding)
		}

		res, err := io.ReadAll(zr)
		if err != nil {
			return nil, fmt.Errorf("gzip copy on %d: %w %q", i, err, encoding)
		}

		if err := zr.Close(); err != nil {
			return nil, fmt.Errorf("gzip close on %d: %w", i, err)
		}

		var h types.Header
		if err := rlp.DecodeBytes(res, &h); err != nil {
			return nil, fmt.Errorf("parsing hard coded header on %d: %w", i, err)
		}

		headerHash := types.RawRlpHash(res)
		hardTips[headerHash] = HeaderRecord{Raw: b, Header: &h}

		buf.Reset()
	}

	return hardTips, nil
}
