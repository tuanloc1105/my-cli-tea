package searcher

import (
	"context"
	"fmt"
	"sync"
)

func runCoordinator(
	ctx context.Context,
	options Options,
	matcher *matcher,
	handle EventHandler,
	fs fileSystem,
) (Summary, error) {
	workerContext, cancel := context.WithCancel(ctx)
	defer cancel()

	candidates := make(chan candidate)
	traversalResult := make(chan error, 1)
	go func() {
		traversalResult <- walkCandidates(workerContext, options, fs, candidates)
	}()

	type job struct {
		events <-chan Event
	}
	jobs := make([]job, 0, options.MaxWorkers)
	walkDone := false
	var workers sync.WaitGroup
	var summary Summary

	launch := func(item candidate) {
		events := make(chan Event)
		workers.Add(1)
		go func() {
			defer workers.Done()
			processCandidate(workerContext, item, options, matcher, fs, events)
		}()
		jobs = append(jobs, job{events: events})
	}

	for {
		for len(jobs) < options.MaxWorkers && !walkDone {
			select {
			case item, ok := <-candidates:
				if !ok {
					walkDone = true
					continue
				}
				launch(item)
			case <-workerContext.Done():
				walkDone = true
			}
		}

		if len(jobs) == 0 {
			if walkDone {
				break
			}
			continue
		}

		for event := range jobs[0].events {
			if event.Result != nil {
				if err := handle(event); err != nil {
					cancel()
					workers.Wait()
					<-traversalResult
					return summary, fmt.Errorf("emit search result: %w", err)
				}
				summary.Matches++
				if options.MaxResults > 0 && summary.Matches == options.MaxResults {
					summary.StoppedEarly = true
					cancel()
					break
				}
			}
			if event.Diagnostic != nil {
				summary.PartialErrors++
				if err := handle(event); err != nil {
					cancel()
					workers.Wait()
					<-traversalResult
					return summary, fmt.Errorf("emit search diagnostic: %w", err)
				}
			}
		}
		jobs = jobs[1:]
		if summary.StoppedEarly {
			break
		}
	}

	cancel()
	workers.Wait()
	traversalErr := <-traversalResult
	if err := ctx.Err(); err != nil && !summary.StoppedEarly {
		return summary, err
	}
	if traversalErr != nil && !summary.StoppedEarly {
		return summary, traversalErr
	}
	return summary, nil
}
