// Code generated by MockGen. DO NOT EDIT.
// Source: github.com/erigontech/erigon/execution/consensus (interfaces: ChainHeaderReader)
//
// Generated by this command:
//
//	mockgen -typed=true -destination=./chain_header_reader_mock.go -package=consensus . ChainHeaderReader
//

// Package consensus is a generated GoMock package.
package consensus

import (
	big "math/big"
	reflect "reflect"

	chain "github.com/erigontech/erigon-lib/chain"
	common "github.com/erigontech/erigon-lib/common"
	types "github.com/erigontech/erigon-lib/types"
	gomock "go.uber.org/mock/gomock"
)

// MockChainHeaderReader is a mock of ChainHeaderReader interface.
type MockChainHeaderReader struct {
	ctrl     *gomock.Controller
	recorder *MockChainHeaderReaderMockRecorder
	isgomock struct{}
}

// MockChainHeaderReaderMockRecorder is the mock recorder for MockChainHeaderReader.
type MockChainHeaderReaderMockRecorder struct {
	mock *MockChainHeaderReader
}

// NewMockChainHeaderReader creates a new mock instance.
func NewMockChainHeaderReader(ctrl *gomock.Controller) *MockChainHeaderReader {
	mock := &MockChainHeaderReader{ctrl: ctrl}
	mock.recorder = &MockChainHeaderReaderMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockChainHeaderReader) EXPECT() *MockChainHeaderReaderMockRecorder {
	return m.recorder
}

// Config mocks base method.
func (m *MockChainHeaderReader) Config() *chain.Config {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Config")
	ret0, _ := ret[0].(*chain.Config)
	return ret0
}

// Config indicates an expected call of Config.
func (mr *MockChainHeaderReaderMockRecorder) Config() *MockChainHeaderReaderConfigCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Config", reflect.TypeOf((*MockChainHeaderReader)(nil).Config))
	return &MockChainHeaderReaderConfigCall{Call: call}
}

// MockChainHeaderReaderConfigCall wrap *gomock.Call
type MockChainHeaderReaderConfigCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockChainHeaderReaderConfigCall) Return(arg0 *chain.Config) *MockChainHeaderReaderConfigCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockChainHeaderReaderConfigCall) Do(f func() *chain.Config) *MockChainHeaderReaderConfigCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockChainHeaderReaderConfigCall) DoAndReturn(f func() *chain.Config) *MockChainHeaderReaderConfigCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// CurrentFinalizedHeader mocks base method.
func (m *MockChainHeaderReader) CurrentFinalizedHeader() *types.Header {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "CurrentFinalizedHeader")
	ret0, _ := ret[0].(*types.Header)
	return ret0
}

// CurrentFinalizedHeader indicates an expected call of CurrentFinalizedHeader.
func (mr *MockChainHeaderReaderMockRecorder) CurrentFinalizedHeader() *MockChainHeaderReaderCurrentFinalizedHeaderCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "CurrentFinalizedHeader", reflect.TypeOf((*MockChainHeaderReader)(nil).CurrentFinalizedHeader))
	return &MockChainHeaderReaderCurrentFinalizedHeaderCall{Call: call}
}

// MockChainHeaderReaderCurrentFinalizedHeaderCall wrap *gomock.Call
type MockChainHeaderReaderCurrentFinalizedHeaderCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockChainHeaderReaderCurrentFinalizedHeaderCall) Return(arg0 *types.Header) *MockChainHeaderReaderCurrentFinalizedHeaderCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockChainHeaderReaderCurrentFinalizedHeaderCall) Do(f func() *types.Header) *MockChainHeaderReaderCurrentFinalizedHeaderCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockChainHeaderReaderCurrentFinalizedHeaderCall) DoAndReturn(f func() *types.Header) *MockChainHeaderReaderCurrentFinalizedHeaderCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// CurrentHeader mocks base method.
func (m *MockChainHeaderReader) CurrentHeader() *types.Header {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "CurrentHeader")
	ret0, _ := ret[0].(*types.Header)
	return ret0
}

// CurrentHeader indicates an expected call of CurrentHeader.
func (mr *MockChainHeaderReaderMockRecorder) CurrentHeader() *MockChainHeaderReaderCurrentHeaderCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "CurrentHeader", reflect.TypeOf((*MockChainHeaderReader)(nil).CurrentHeader))
	return &MockChainHeaderReaderCurrentHeaderCall{Call: call}
}

// MockChainHeaderReaderCurrentHeaderCall wrap *gomock.Call
type MockChainHeaderReaderCurrentHeaderCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockChainHeaderReaderCurrentHeaderCall) Return(arg0 *types.Header) *MockChainHeaderReaderCurrentHeaderCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockChainHeaderReaderCurrentHeaderCall) Do(f func() *types.Header) *MockChainHeaderReaderCurrentHeaderCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockChainHeaderReaderCurrentHeaderCall) DoAndReturn(f func() *types.Header) *MockChainHeaderReaderCurrentHeaderCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// CurrentSafeHeader mocks base method.
func (m *MockChainHeaderReader) CurrentSafeHeader() *types.Header {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "CurrentSafeHeader")
	ret0, _ := ret[0].(*types.Header)
	return ret0
}

// CurrentSafeHeader indicates an expected call of CurrentSafeHeader.
func (mr *MockChainHeaderReaderMockRecorder) CurrentSafeHeader() *MockChainHeaderReaderCurrentSafeHeaderCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "CurrentSafeHeader", reflect.TypeOf((*MockChainHeaderReader)(nil).CurrentSafeHeader))
	return &MockChainHeaderReaderCurrentSafeHeaderCall{Call: call}
}

// MockChainHeaderReaderCurrentSafeHeaderCall wrap *gomock.Call
type MockChainHeaderReaderCurrentSafeHeaderCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockChainHeaderReaderCurrentSafeHeaderCall) Return(arg0 *types.Header) *MockChainHeaderReaderCurrentSafeHeaderCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockChainHeaderReaderCurrentSafeHeaderCall) Do(f func() *types.Header) *MockChainHeaderReaderCurrentSafeHeaderCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockChainHeaderReaderCurrentSafeHeaderCall) DoAndReturn(f func() *types.Header) *MockChainHeaderReaderCurrentSafeHeaderCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// FrozenBlocks mocks base method.
func (m *MockChainHeaderReader) FrozenBlocks() uint64 {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "FrozenBlocks")
	ret0, _ := ret[0].(uint64)
	return ret0
}

// FrozenBlocks indicates an expected call of FrozenBlocks.
func (mr *MockChainHeaderReaderMockRecorder) FrozenBlocks() *MockChainHeaderReaderFrozenBlocksCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "FrozenBlocks", reflect.TypeOf((*MockChainHeaderReader)(nil).FrozenBlocks))
	return &MockChainHeaderReaderFrozenBlocksCall{Call: call}
}

// MockChainHeaderReaderFrozenBlocksCall wrap *gomock.Call
type MockChainHeaderReaderFrozenBlocksCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockChainHeaderReaderFrozenBlocksCall) Return(arg0 uint64) *MockChainHeaderReaderFrozenBlocksCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockChainHeaderReaderFrozenBlocksCall) Do(f func() uint64) *MockChainHeaderReaderFrozenBlocksCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockChainHeaderReaderFrozenBlocksCall) DoAndReturn(f func() uint64) *MockChainHeaderReaderFrozenBlocksCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// FrozenBorBlocks mocks base method.
func (m *MockChainHeaderReader) FrozenBorBlocks(align bool) uint64 {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "FrozenBorBlocks", align)
	ret0, _ := ret[0].(uint64)
	return ret0
}

// FrozenBorBlocks indicates an expected call of FrozenBorBlocks.
func (mr *MockChainHeaderReaderMockRecorder) FrozenBorBlocks(align any) *MockChainHeaderReaderFrozenBorBlocksCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "FrozenBorBlocks", reflect.TypeOf((*MockChainHeaderReader)(nil).FrozenBorBlocks), align)
	return &MockChainHeaderReaderFrozenBorBlocksCall{Call: call}
}

// MockChainHeaderReaderFrozenBorBlocksCall wrap *gomock.Call
type MockChainHeaderReaderFrozenBorBlocksCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockChainHeaderReaderFrozenBorBlocksCall) Return(arg0 uint64) *MockChainHeaderReaderFrozenBorBlocksCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockChainHeaderReaderFrozenBorBlocksCall) Do(f func(bool) uint64) *MockChainHeaderReaderFrozenBorBlocksCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockChainHeaderReaderFrozenBorBlocksCall) DoAndReturn(f func(bool) uint64) *MockChainHeaderReaderFrozenBorBlocksCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// GetHeader mocks base method.
func (m *MockChainHeaderReader) GetHeader(hash common.Hash, number uint64) *types.Header {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetHeader", hash, number)
	ret0, _ := ret[0].(*types.Header)
	return ret0
}

// GetHeader indicates an expected call of GetHeader.
func (mr *MockChainHeaderReaderMockRecorder) GetHeader(hash, number any) *MockChainHeaderReaderGetHeaderCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetHeader", reflect.TypeOf((*MockChainHeaderReader)(nil).GetHeader), hash, number)
	return &MockChainHeaderReaderGetHeaderCall{Call: call}
}

// MockChainHeaderReaderGetHeaderCall wrap *gomock.Call
type MockChainHeaderReaderGetHeaderCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockChainHeaderReaderGetHeaderCall) Return(arg0 *types.Header) *MockChainHeaderReaderGetHeaderCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockChainHeaderReaderGetHeaderCall) Do(f func(common.Hash, uint64) *types.Header) *MockChainHeaderReaderGetHeaderCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockChainHeaderReaderGetHeaderCall) DoAndReturn(f func(common.Hash, uint64) *types.Header) *MockChainHeaderReaderGetHeaderCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// GetHeaderByHash mocks base method.
func (m *MockChainHeaderReader) GetHeaderByHash(hash common.Hash) *types.Header {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetHeaderByHash", hash)
	ret0, _ := ret[0].(*types.Header)
	return ret0
}

// GetHeaderByHash indicates an expected call of GetHeaderByHash.
func (mr *MockChainHeaderReaderMockRecorder) GetHeaderByHash(hash any) *MockChainHeaderReaderGetHeaderByHashCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetHeaderByHash", reflect.TypeOf((*MockChainHeaderReader)(nil).GetHeaderByHash), hash)
	return &MockChainHeaderReaderGetHeaderByHashCall{Call: call}
}

// MockChainHeaderReaderGetHeaderByHashCall wrap *gomock.Call
type MockChainHeaderReaderGetHeaderByHashCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockChainHeaderReaderGetHeaderByHashCall) Return(arg0 *types.Header) *MockChainHeaderReaderGetHeaderByHashCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockChainHeaderReaderGetHeaderByHashCall) Do(f func(common.Hash) *types.Header) *MockChainHeaderReaderGetHeaderByHashCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockChainHeaderReaderGetHeaderByHashCall) DoAndReturn(f func(common.Hash) *types.Header) *MockChainHeaderReaderGetHeaderByHashCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// GetHeaderByNumber mocks base method.
func (m *MockChainHeaderReader) GetHeaderByNumber(number uint64) *types.Header {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetHeaderByNumber", number)
	ret0, _ := ret[0].(*types.Header)
	return ret0
}

// GetHeaderByNumber indicates an expected call of GetHeaderByNumber.
func (mr *MockChainHeaderReaderMockRecorder) GetHeaderByNumber(number any) *MockChainHeaderReaderGetHeaderByNumberCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetHeaderByNumber", reflect.TypeOf((*MockChainHeaderReader)(nil).GetHeaderByNumber), number)
	return &MockChainHeaderReaderGetHeaderByNumberCall{Call: call}
}

// MockChainHeaderReaderGetHeaderByNumberCall wrap *gomock.Call
type MockChainHeaderReaderGetHeaderByNumberCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockChainHeaderReaderGetHeaderByNumberCall) Return(arg0 *types.Header) *MockChainHeaderReaderGetHeaderByNumberCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockChainHeaderReaderGetHeaderByNumberCall) Do(f func(uint64) *types.Header) *MockChainHeaderReaderGetHeaderByNumberCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockChainHeaderReaderGetHeaderByNumberCall) DoAndReturn(f func(uint64) *types.Header) *MockChainHeaderReaderGetHeaderByNumberCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}

// GetTd mocks base method.
func (m *MockChainHeaderReader) GetTd(hash common.Hash, number uint64) *big.Int {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetTd", hash, number)
	ret0, _ := ret[0].(*big.Int)
	return ret0
}

// GetTd indicates an expected call of GetTd.
func (mr *MockChainHeaderReaderMockRecorder) GetTd(hash, number any) *MockChainHeaderReaderGetTdCall {
	mr.mock.ctrl.T.Helper()
	call := mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetTd", reflect.TypeOf((*MockChainHeaderReader)(nil).GetTd), hash, number)
	return &MockChainHeaderReaderGetTdCall{Call: call}
}

// MockChainHeaderReaderGetTdCall wrap *gomock.Call
type MockChainHeaderReaderGetTdCall struct {
	*gomock.Call
}

// Return rewrite *gomock.Call.Return
func (c *MockChainHeaderReaderGetTdCall) Return(arg0 *big.Int) *MockChainHeaderReaderGetTdCall {
	c.Call = c.Call.Return(arg0)
	return c
}

// Do rewrite *gomock.Call.Do
func (c *MockChainHeaderReaderGetTdCall) Do(f func(common.Hash, uint64) *big.Int) *MockChainHeaderReaderGetTdCall {
	c.Call = c.Call.Do(f)
	return c
}

// DoAndReturn rewrite *gomock.Call.DoAndReturn
func (c *MockChainHeaderReaderGetTdCall) DoAndReturn(f func(common.Hash, uint64) *big.Int) *MockChainHeaderReaderGetTdCall {
	c.Call = c.Call.DoAndReturn(f)
	return c
}
