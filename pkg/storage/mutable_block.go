package storage

import (
	"sort"
	"sync"

	"github.com/openzipkin/zipkin-go-opentracing/_thrift/gen-go/zipkincore"
)

const mutableTraces = 1024

type mutableBlock struct {
	mtx       sync.RWMutex
	traces    map[int64]*Trace
	services  map[string]struct{}
	spanNames map[string]map[string]struct{}
}

func newMutableBlock() *mutableBlock {
	return &mutableBlock{
		traces:    make(map[int64]*Trace, mutableTraces),
		services:  map[string]struct{}{},
		spanNames: map[string]map[string]struct{}{},
	}
}

func (s *mutableBlock) Full() bool {
	s.mtx.RLock()
	defer s.mtx.RUnlock()
	return len(s.traces) >= mutableTraces
}

func (s *mutableBlock) Append(span *zipkincore.Span) error {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	traceID := span.GetTraceID()

	t, ok := s.traces[traceID]
	if !ok {
		t = &Trace{}
		s.traces[traceID] = t
	}

	t.Spans = append(t.Spans, span)
	sort.Sort(byTimestamp(t.Spans))

	if t.MinTimestamp > span.GetTimestamp() {
		t.MinTimestamp = span.GetTimestamp()
	}

	// update services 'index'
	services := map[string]struct{}{}
	for _, annotation := range span.Annotations {
		s.services[annotation.Host.ServiceName] = struct{}{}
		services[annotation.Host.ServiceName] = struct{}{}
	}
	for _, annotation := range span.BinaryAnnotations {
		s.services[annotation.Host.ServiceName] = struct{}{}
		services[annotation.Host.ServiceName] = struct{}{}
	}

	// update spanNames 'index'
	for service := range services {
		if _, ok := s.spanNames[service]; !ok {
			s.spanNames[service] = map[string]struct{}{}
		}
		s.spanNames[service][span.Name] = struct{}{}
	}

	return nil
}

func (s *mutableBlock) Services() ([]string, error) {
	s.mtx.RLock()
	defer s.mtx.RUnlock()
	result := make([]string, 0, len(s.services))
	for service := range s.services {
		result = append(result, service)
	}
	return result, nil
}

func (s *mutableBlock) SpanNames(serviceName string) ([]string, error) {
	s.mtx.RLock()
	defer s.mtx.RUnlock()
	names, ok := s.spanNames[serviceName]
	if !ok {
		return nil, nil
	}
	result := make([]string, 0, len(names))
	for name := range names {
		result = append(result, name)
	}
	return result, nil
}

func (s *mutableBlock) Trace(id int64) (Trace, error) {
	s.mtx.RLock()
	defer s.mtx.RUnlock()

	trace, ok := s.traces[id]
	if !ok {
		return Trace{}, nil
	}
	return *trace, nil
}

func (s *mutableBlock) Traces(query Query) ([]Trace, error) {
	s.mtx.RLock()
	defer s.mtx.RUnlock()

	traces := []Trace{}
	for _, trace := range s.traces {
		if trace.match(query) {
			traces = append(traces, *trace)
		}
	}
	sort.Sort(byMinTimestamp(traces))
	if query.Limit > 0 && len(traces) > query.Limit {
		traces = traces[len(traces)-query.Limit:]
	}
	return traces, nil
}
