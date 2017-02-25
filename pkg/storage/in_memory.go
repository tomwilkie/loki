package storage

import (
	"sort"
	"sync"

	"github.com/openzipkin/zipkin-go-opentracing/_thrift/gen-go/zipkincore"
)

const numImmutableBlocks = 1024

func NewSpanStore() SpanStore {
	return &inMemory{
		mutableBlock: newMutableBlock(),
	}
}

type inMemory struct {
	mtx             sync.RWMutex
	mutableBlock    *mutableBlock
	immutableBlocks []*immutableBlock
}

func (s *inMemory) Append(span *zipkincore.Span) error {
	var err error
	s.mtx.RLock()
	full := s.mutableBlock.Full()
	if !full {
		err = s.mutableBlock.Append(span)
	}
	s.mtx.RUnlock()

	if !full {
		return err
	}

	// mutableBlock was full, so swap it out for a new one
	s.mtx.Lock()
	defer s.mtx.Unlock()

	s.immutableBlocks = append(s.immutableBlocks, newImmutableBlock(s.mutableBlock))
	if len(s.immutableBlocks) > numImmutableBlocks {
		s.immutableBlocks = s.immutableBlocks[1:]
	}
	s.mutableBlock = newMutableBlock()
	return s.mutableBlock.Append(span)
}

func (s *inMemory) stores(f func(ReadStore) error) error {
	s.mtx.RLock()
	defer s.mtx.RUnlock()
	if err := f(s.mutableBlock); err != nil {
		return err
	}
	for _, b := range s.immutableBlocks {
		if err := f(b); err != nil {
			return err
		}
	}
	return nil
}

func (s *inMemory) Services() ([]string, error) {
	var result [][]string
	err := s.stores(func(s ReadStore) error {
		services, err := s.Services()
		sort.Strings(services)
		result = append(result, services)
		return err
	})
	return mergeStringListList(result), err
}

func (s *inMemory) SpanNames(serviceName string) ([]string, error) {
	var result [][]string
	err := s.stores(func(s ReadStore) error {
		names, err := s.SpanNames(serviceName)
		sort.Strings(names)
		result = append(result, names)
		return err
	})
	return mergeStringListList(result), err
}

func (s *inMemory) Trace(id int64) (Trace, error) {
	var result []Trace
	err := s.stores(func(s ReadStore) error {
		trace, err := s.Trace(id)
		result = append(result, trace)
		return err
	})
	return mergeTraceList(result), err
}

func (s *inMemory) Traces(query Query) ([]Trace, error) {
	var result [][]Trace
	err := s.stores(func(s ReadStore) error {
		traces, err := s.Traces(query)
		result = append(result, traces)
		return err
	})
	traces := mergeTraceListList(result)
	if query.Limit > 0 && len(traces) > query.Limit {
		traces = traces[len(traces)-query.Limit:]
	}
	return traces, err
}
