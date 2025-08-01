// Copyright 2017 The go-ethereum Authors
// (original work)
// Copyright 2024 The Erigon Authors
// (modifications)
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

package clique_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"sort"
	"testing"

	"github.com/jinzhu/copier"

	"github.com/erigontech/erigon-lib/chain"
	"github.com/erigontech/erigon-lib/common"
	"github.com/erigontech/erigon-lib/common/length"
	"github.com/erigontech/erigon-lib/crypto"
	"github.com/erigontech/erigon-lib/kv"
	"github.com/erigontech/erigon-lib/kv/memdb"
	"github.com/erigontech/erigon-lib/log/v3"
	"github.com/erigontech/erigon-lib/testlog"
	"github.com/erigontech/erigon-lib/types"
	"github.com/erigontech/erigon/core"
	"github.com/erigontech/erigon/execution/chainspec"
	"github.com/erigontech/erigon/execution/consensus/clique"
	"github.com/erigontech/erigon/execution/stagedsync"
	"github.com/erigontech/erigon/execution/stages/mock"
)

// testerAccountPool is a pool to maintain currently active tester accounts,
// mapped from textual names used in the tests below to actual Ethereum private
// keys capable of signing transactions.
type testerAccountPool struct {
	accounts map[string]*ecdsa.PrivateKey
}

func newTesterAccountPool() *testerAccountPool {
	return &testerAccountPool{
		accounts: make(map[string]*ecdsa.PrivateKey),
	}
}

// checkpoint creates a Clique checkpoint signer section from the provided list
// of authorized signers and embeds it into the provided header.
func (ap *testerAccountPool) checkpoint(header *types.Header, signers []string) {
	auths := make([]common.Address, len(signers))
	for i, signer := range signers {
		auths[i] = ap.address(signer)
	}
	sort.Sort(clique.SignersAscending(auths))
	for i, auth := range auths {
		copy(header.Extra[clique.ExtraVanity+i*length.Addr:], auth.Bytes())
	}
}

// address retrieves the Ethereum address of a tester account by label, creating
// a new account if no previous one exists yet.
func (ap *testerAccountPool) address(account string) common.Address {
	// Return the zero account for non-addresses
	if account == "" {
		return common.Address{}
	}
	// Ensure we have a persistent key for the account
	if ap.accounts[account] == nil {
		ap.accounts[account], _ = crypto.GenerateKey()
	}
	// Resolve and return the Ethereum address
	return crypto.PubkeyToAddress(ap.accounts[account].PublicKey)
}

// sign calculates a Clique digital signature for the given block and embeds it
// back into the header.
func (ap *testerAccountPool) sign(header *types.Header, signer string) {
	// Ensure we have a persistent key for the signer
	if ap.accounts[signer] == nil {
		ap.accounts[signer], _ = crypto.GenerateKey()
	}
	// Sign the header and embed the signature in extra data
	sig, _ := crypto.Sign(clique.SealHash(header).Bytes(), ap.accounts[signer])
	copy(header.Extra[len(header.Extra)-clique.ExtraSeal:], sig)
}

// testerVote represents a single block signed by a parcitular account, where
// the account may or may not have cast a Clique vote.
type testerVote struct {
	signer     string
	voted      string
	auth       bool
	checkpoint []string
	newbatch   bool
}

// Tests that Clique signer voting is evaluated correctly for various simple and
// complex scenarios, as well as that a few special corner cases fail correctly.
func TestClique(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}

	// Define the various voting scenarios to test
	tests := []struct {
		name    string
		epoch   uint64
		signers []string
		votes   []testerVote
		results []string
		failure error
	}{
		{
			name:    "Single signer, no votes cast",
			signers: []string{"A"},
			votes:   []testerVote{{signer: "A"}},
			results: []string{"A"},
		}, {
			name:    "Single signer, voting to add two others (only accept first, second needs 2 votes)",
			signers: []string{"A"},
			votes: []testerVote{
				{signer: "A", voted: "B", auth: true},
				{signer: "B"},
				{signer: "A", voted: "C", auth: true},
			},
			results: []string{"A", "B"},
		}, {
			name:    "Two signers, voting to add three others (only accept first two, third needs 3 votes already)",
			signers: []string{"A", "B"},
			votes: []testerVote{
				{signer: "A", voted: "C", auth: true},
				{signer: "B", voted: "C", auth: true},
				{signer: "A", voted: "D", auth: true},
				{signer: "B", voted: "D", auth: true},
				{signer: "C"},
				{signer: "A", voted: "E", auth: true},
				{signer: "B", voted: "E", auth: true},
			},
			results: []string{"A", "B", "C", "D"},
		}, {
			name:    "Single signer, dropping itself (weird, but one less cornercase by explicitly allowing this)",
			signers: []string{"A"},
			votes: []testerVote{
				{signer: "A", voted: "A", auth: false},
			},
			results: []string{},
		}, {
			name:    "Two signers, actually needing mutual consent to drop either of them (not fulfilled)",
			signers: []string{"A", "B"},
			votes: []testerVote{
				{signer: "A", voted: "B", auth: false},
			},
			results: []string{"A", "B"},
		}, {
			name:    "Two signers, actually needing mutual consent to drop either of them (fulfilled)",
			signers: []string{"A", "B"},
			votes: []testerVote{
				{signer: "A", voted: "B", auth: false},
				{signer: "B", voted: "B", auth: false},
			},
			results: []string{"A"},
		}, {
			name:    "Three signers, two of them deciding to drop the third",
			signers: []string{"A", "B", "C"},
			votes: []testerVote{
				{signer: "A", voted: "C", auth: false},
				{signer: "B", voted: "C", auth: false},
			},
			results: []string{"A", "B"},
		}, {
			name:    "Four signers, consensus of two not being enough to drop anyone",
			signers: []string{"A", "B", "C", "D"},
			votes: []testerVote{
				{signer: "A", voted: "C", auth: false},
				{signer: "B", voted: "C", auth: false},
			},
			results: []string{"A", "B", "C", "D"},
		}, {
			name:    "Four signers, consensus of three already being enough to drop someone",
			signers: []string{"A", "B", "C", "D"},
			votes: []testerVote{
				{signer: "A", voted: "D", auth: false},
				{signer: "B", voted: "D", auth: false},
				{signer: "C", voted: "D", auth: false},
			},
			results: []string{"A", "B", "C"},
		}, {
			name:    "Authorizations are counted once per signer per target",
			signers: []string{"A", "B"},
			votes: []testerVote{
				{signer: "A", voted: "C", auth: true},
				{signer: "B"},
				{signer: "A", voted: "C", auth: true},
				{signer: "B"},
				{signer: "A", voted: "C", auth: true},
			},
			results: []string{"A", "B"},
		}, {
			name:    "Authorizing multiple accounts concurrently is permitted",
			signers: []string{"A", "B"},
			votes: []testerVote{
				{signer: "A", voted: "C", auth: true},
				{signer: "B"},
				{signer: "A", voted: "D", auth: true},
				{signer: "B"},
				{signer: "A"},
				{signer: "B", voted: "D", auth: true},
				{signer: "A"},
				{signer: "B", voted: "C", auth: true},
			},
			results: []string{"A", "B", "C", "D"},
		}, {
			name:    "Deauthorizations are counted once per signer per target",
			signers: []string{"A", "B"},
			votes: []testerVote{
				{signer: "A", voted: "B", auth: false},
				{signer: "B"},
				{signer: "A", voted: "B", auth: false},
				{signer: "B"},
				{signer: "A", voted: "B", auth: false},
			},
			results: []string{"A", "B"},
		}, {
			name:    "Deauthorizing multiple accounts concurrently is permitted",
			signers: []string{"A", "B", "C", "D"},
			votes: []testerVote{
				{signer: "A", voted: "C", auth: false},
				{signer: "B"},
				{signer: "C"},
				{signer: "A", voted: "D", auth: false},
				{signer: "B"},
				{signer: "C"},
				{signer: "A"},
				{signer: "B", voted: "D", auth: false},
				{signer: "C", voted: "D", auth: false},
				{signer: "A"},
				{signer: "B", voted: "C", auth: false},
			},
			results: []string{"A", "B"},
		}, {
			name:    "Votes from deauthorized signers are discarded immediately (deauth votes)",
			signers: []string{"A", "B", "C"},
			votes: []testerVote{
				{signer: "C", voted: "B", auth: false},
				{signer: "A", voted: "C", auth: false},
				{signer: "B", voted: "C", auth: false},
				{signer: "A", voted: "B", auth: false},
			},
			results: []string{"A", "B"},
		}, {
			name:    "Votes from deauthorized signers are discarded immediately (auth votes)",
			signers: []string{"A", "B", "C"},
			votes: []testerVote{
				{signer: "C", voted: "D", auth: true},
				{signer: "A", voted: "C", auth: false},
				{signer: "B", voted: "C", auth: false},
				{signer: "A", voted: "D", auth: true},
			},
			results: []string{"A", "B"},
		}, {
			name:    "Cascading changes are not allowed, only the account being voted on may change",
			signers: []string{"A", "B", "C", "D"},
			votes: []testerVote{
				{signer: "A", voted: "C", auth: false},
				{signer: "B"},
				{signer: "C"},
				{signer: "A", voted: "D", auth: false},
				{signer: "B", voted: "C", auth: false},
				{signer: "C"},
				{signer: "A"},
				{signer: "B", voted: "D", auth: false},
				{signer: "C", voted: "D", auth: false},
			},
			results: []string{"A", "B", "C"},
		}, {
			name:    "Changes reaching consensus out of bounds (via a deauth) execute on touch",
			signers: []string{"A", "B", "C", "D"},
			votes: []testerVote{
				{signer: "A", voted: "C", auth: false},
				{signer: "B"},
				{signer: "C"},
				{signer: "A", voted: "D", auth: false},
				{signer: "B", voted: "C", auth: false},
				{signer: "C"},
				{signer: "A"},
				{signer: "B", voted: "D", auth: false},
				{signer: "C", voted: "D", auth: false},
				{signer: "A"},
				{signer: "C", voted: "C", auth: true},
			},
			results: []string{"A", "B"},
		}, {
			name:    "Changes reaching consensus out of bounds (via a deauth) may go out of consensus on first touch",
			signers: []string{"A", "B", "C", "D"},
			votes: []testerVote{
				{signer: "A", voted: "C", auth: false},
				{signer: "B"},
				{signer: "C"},
				{signer: "A", voted: "D", auth: false},
				{signer: "B", voted: "C", auth: false},
				{signer: "C"},
				{signer: "A"},
				{signer: "B", voted: "D", auth: false},
				{signer: "C", voted: "D", auth: false},
				{signer: "A"},
				{signer: "B", voted: "C", auth: true},
			},
			results: []string{"A", "B", "C"},
		}, {
			// Ensure that pending votes don't survive authorization status changes. This
			// corner case can only appear if a signer is quickly added, removed and then
			// readded (or the inverse), while one of the original voters dropped. If a
			// past vote is left cached in the system somewhere, this will interfere with
			// the final signer outcome.
			name:    "pending votes don't survive authorization status changes",
			signers: []string{"A", "B", "C", "D", "E"},
			votes: []testerVote{
				{signer: "A", voted: "F", auth: true}, // Authorize F, 3 votes needed
				{signer: "B", voted: "F", auth: true},
				{signer: "C", voted: "F", auth: true},
				{signer: "D", voted: "F", auth: false}, // Deauthorize F, 4 votes needed (leave A's previous vote "unchanged")
				{signer: "E", voted: "F", auth: false},
				{signer: "B", voted: "F", auth: false},
				{signer: "C", voted: "F", auth: false},
				{signer: "D", voted: "F", auth: true}, // Almost authorize F, 2/3 votes needed
				{signer: "E", voted: "F", auth: true},
				{signer: "B", voted: "A", auth: false}, // Deauthorize A, 3 votes needed
				{signer: "C", voted: "A", auth: false},
				{signer: "D", voted: "A", auth: false},
				{signer: "B", voted: "F", auth: true}, // Finish authorizing F, 3/3 votes needed
			},
			results: []string{"B", "C", "D", "E", "F"},
		}, {
			name:    "Epoch transitions reset all votes to allow chain checkpointing",
			epoch:   3,
			signers: []string{"A", "B"},
			votes: []testerVote{
				{signer: "A", voted: "C", auth: true},
				{signer: "B"},
				{signer: "A", checkpoint: []string{"A", "B"}},
				{signer: "B", voted: "C", auth: true},
			},
			results: []string{"A", "B"},
		}, {
			name:    "An unauthorized signer should not be able to sign blocks",
			signers: []string{"A"},
			votes: []testerVote{
				{signer: "B"},
			},
			failure: clique.ErrUnauthorizedSigner,
		}, {
			name:    "An authorized signer that signed recenty should not be able to sign again",
			signers: []string{"A", "B"},
			votes: []testerVote{
				{signer: "A"},
				{signer: "A"},
			},
			failure: clique.ErrRecentlySigned,
		}, {
			name:    "Recent signatures should not reset on checkpoint blocks imported in a batch",
			epoch:   3,
			signers: []string{"A", "B", "C"},
			votes: []testerVote{
				{signer: "A"},
				{signer: "B"},
				{signer: "A", checkpoint: []string{"A", "B", "C"}},
				{signer: "A"},
			},
			failure: clique.ErrRecentlySigned,
		}, {
			// Recent signatures should not reset on checkpoint blocks imported in a new
			// batch (https://github.com/erigontech/erigon/issues/17593). Whilst this
			// seems overly specific and weird, it was a Rinkeby consensus split.
			name:    "Recent signatures should not reset on checkpoint blocks imported in a new batch",
			epoch:   3,
			signers: []string{"A", "B", "C"},
			votes: []testerVote{
				{signer: "A"},
				{signer: "B"},
				{signer: "A", checkpoint: []string{"A", "B", "C"}},
				{signer: "A", newbatch: true},
			},
			failure: clique.ErrRecentlySigned,
		},
	}
	// Run through the scenarios and test them
	for i, tt := range tests {
		i := i
		tt := tt

		t.Run(tt.name, func(t *testing.T) {
			logger := testlog.Logger(t, log.LvlInfo)
			// Create the account pool and generate the initial set of signers
			accounts := newTesterAccountPool()

			signers := make([]common.Address, len(tt.signers))
			for j, signer := range tt.signers {
				signers[j] = accounts.address(signer)
			}
			for j := 0; j < len(signers); j++ {
				for k := j + 1; k < len(signers); k++ {
					if bytes.Compare(signers[j][:], signers[k][:]) > 0 {
						signers[j], signers[k] = signers[k], signers[j]
					}
				}
			}
			// Create the genesis block with the initial set of signers
			genesis := &types.Genesis{
				ExtraData: make([]byte, clique.ExtraVanity+length.Addr*len(signers)+clique.ExtraSeal),
				Config:    chainspec.AllCliqueProtocolChanges,
			}
			for j, signer := range signers {
				copy(genesis.ExtraData[clique.ExtraVanity+j*length.Addr:], signer[:])
			}

			// Assemble a chain of headers from the cast votes
			var config chain.Config
			copier.Copy(&config, chainspec.AllCliqueProtocolChanges)
			config.Clique = &chain.CliqueConfig{
				Period: 1,
				Epoch:  tt.epoch,
			}

			cliqueDB := memdb.NewTestDB(t, kv.ConsensusDB)

			engine := clique.New(&config, chainspec.CliqueSnapshot, cliqueDB, log.New())
			engine.FakeDiff = true
			checkStateRoot := true
			// Create a pristine blockchain with the genesis injected
			m := mock.MockWithGenesisEngine(t, genesis, engine, false, checkStateRoot)

			chain, err := core.GenerateChain(m.ChainConfig, m.Genesis, m.Engine, m.DB, len(tt.votes), func(j int, gen *core.BlockGen) {
				// Cast the vote contained in this block
				gen.SetCoinbase(accounts.address(tt.votes[j].voted))
				if tt.votes[j].auth {
					var nonce types.BlockNonce
					copy(nonce[:], clique.NonceAuthVote)
					gen.SetNonce(nonce)
				}
			})
			if err != nil {
				t.Fatalf("generate blocks: %v", err)
			}
			// Iterate through the blocks and seal them individually
			for j, block := range chain.Blocks {
				// Get the header and prepare it for signing
				header := block.Header()
				if j > 0 {
					header.ParentHash = chain.Blocks[j-1].Hash()
				}
				header.Extra = make([]byte, clique.ExtraVanity+clique.ExtraSeal)
				if auths := tt.votes[j].checkpoint; auths != nil {
					header.Extra = make([]byte, clique.ExtraVanity+len(auths)*length.Addr+clique.ExtraSeal)
					accounts.checkpoint(header, auths)
				}
				header.Difficulty = clique.DiffInTurn // Ignored, we just need a valid number

				// Generate the signature, embed it into the header and the block
				accounts.sign(header, tt.votes[j].signer)
				chain.Blocks[j] = block.WithSeal(header)
			}
			// Split the blocks up into individual import batches (cornercase testing)
			batches := [][]*types.Block{nil}
			for j, block := range chain.Blocks {
				if tt.votes[j].newbatch {
					batches = append(batches, nil)
				}
				batches[len(batches)-1] = append(batches[len(batches)-1], block)
			}
			// Pass all the headers through clique and ensure tallying succeeds
			failed := false
			for j := 0; j < len(batches)-1; j++ {
				chainX := &core.ChainPack{Blocks: batches[j]}
				chainX.Headers = make([]*types.Header, len(batches[j]))
				for k, b := range batches[j] {
					chainX.Headers[k] = b.Header()
				}
				chainX.TopBlock = batches[j][len(batches[j])-1]
				if err = m.InsertChain(chainX); err != nil {
					t.Errorf("test %d: failed to import batch %d, %v", i, j, err)
					failed = true
					break
				}
			}
			if failed {
				engine.Close()
				return
			}
			chainX := &core.ChainPack{Blocks: batches[len(batches)-1]}
			chainX.Headers = make([]*types.Header, len(batches[len(batches)-1]))
			for k, b := range batches[len(batches)-1] {
				chainX.Headers[k] = b.Header()
			}
			chainX.TopBlock = batches[len(batches)-1][len(batches[len(batches)-1])-1]
			err = m.InsertChain(chainX)
			if tt.failure != nil && err == nil {
				t.Errorf("test %d: expected failure", i)
			}
			if tt.failure == nil && err != nil {
				t.Errorf("test %d: unexpected failure: %v", i, err)
			}
			if tt.failure != nil {
				engine.Close()
				return
			}
			// No failure was produced or requested, generate the final voting snapshot
			head := chain.Blocks[len(chain.Blocks)-1]

			var snap *clique.Snapshot
			if err := m.DB.View(context.Background(), func(tx kv.Tx) error {
				chainReader := stagedsync.ChainReader{
					Cfg:         &config,
					Db:          tx,
					BlockReader: m.BlockReader,
					Logger:      logger,
				}
				snap, err = engine.Snapshot(chainReader, head.NumberU64(), head.Hash(), nil)
				if err != nil {
					return err
				}
				return nil
			}); err != nil {
				t.Errorf("test %d: failed to retrieve voting snapshot %d(%s): %v",
					i, head.NumberU64(), head.Hash().Hex(), err)
				engine.Close()
				return
			}

			// Verify the final list of signers against the expected ones
			signers = make([]common.Address, len(tt.results))
			for j, signer := range tt.results {
				signers[j] = accounts.address(signer)
			}
			for j := 0; j < len(signers); j++ {
				for k := j + 1; k < len(signers); k++ {
					if bytes.Compare(signers[j][:], signers[k][:]) > 0 {
						signers[j], signers[k] = signers[k], signers[j]
					}
				}
			}
			result := snap.GetSigners()
			if len(result) != len(signers) {
				t.Errorf("test %d: signers mismatch: have %x, want %x", i, result, signers)
				engine.Close()
				return
			}
			for j := 0; j < len(result); j++ {
				if !bytes.Equal(result[j][:], signers[j][:]) {
					t.Errorf("test %d, signer %d: signer mismatch: have %x, want %x", i, j, result[j], signers[j])
				}
			}
			engine.Close()
		})
	}
}
