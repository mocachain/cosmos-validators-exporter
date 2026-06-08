package controller

import (
	"context"
	"main/pkg/constants"
	fetchersPkg "main/pkg/fetchers"
	statePkg "main/pkg/state"
	"main/pkg/types"
	"sync"

	"github.com/rs/zerolog"
)

type FetchersStatuses map[constants.FetcherName]bool

func (s FetchersStatuses) IsAllDone(fetcherNames []constants.FetcherName) bool {
	for _, fetcherName := range fetcherNames {
		if _, ok := s[fetcherName]; !ok {
			return false
		}
	}

	return true
}

type Controller struct {
	Fetchers fetchersPkg.Fetchers
	Logger   zerolog.Logger
}

func NewController(
	fetchers fetchersPkg.Fetchers,
	logger *zerolog.Logger,
) *Controller {
	return &Controller{
		Logger: logger.With().
			Str("component", "controller").
			Logger(),
		Fetchers: fetchers,
	}
}

func (c *Controller) Fetch(ctx context.Context) (
	*statePkg.State,
	[]*types.QueryInfo,
) {
	data := statePkg.NewState()
	queries := []*types.QueryInfo{}
	fetchersStatus := FetchersStatuses{}

	var (
		mutex sync.Mutex
		wg    sync.WaitGroup
	)

	processFetcher := func(fetcher fetchersPkg.Fetcher) {
		defer wg.Done()
		// Recover panics from individual fetchers so one bad fetcher
		// (e.g. an Int64 overflow on a chain with 18-decimal precision,
		// or any other runtime panic deep in the cosmos-sdk types) doesn't
		// take down the entire process. Without this, the goroutine panic
		// propagates to the runtime and exits the container with code 2.
		// We still mark the fetcher as done so the outer loop doesn't
		// hang waiting for it.
		defer func() {
			if r := recover(); r != nil {
				c.Logger.Error().
					Interface("panic", r).
					Str("name", string(fetcher.Name())).
					Msg("Fetcher panicked — recovered to keep the process alive")
				mutex.Lock()
				fetchersStatus[fetcher.Name()] = true
				mutex.Unlock()
			}
		}()

		c.Logger.Trace().Str("name", string(fetcher.Name())).Msg("Processing fetcher...")

		mutex.Lock()
		fetcherDependenciesData := data.GetData(fetcher.Dependencies())
		mutex.Unlock()

		fetcherData, fetcherQueries := fetcher.Fetch(ctx, fetcherDependenciesData...)

		mutex.Lock()
		data.Set(fetcher.Name(), fetcherData)

		queries = append(queries, fetcherQueries...)
		fetchersStatus[fetcher.Name()] = true
		mutex.Unlock()

		c.Logger.Trace().
			Str("name", string(fetcher.Name())).
			Msg("Processed fetcher")
	}

	for {
		c.Logger.Trace().Msg("Processing all pending fetchers...")

		if fetchersStatus.IsAllDone(c.Fetchers.GetNames()) {
			c.Logger.Trace().Msg("All fetchers are fetched.")
			break
		}

		fetchersToStart := fetchersPkg.Fetchers{}

		for _, fetcher := range c.Fetchers {
			if _, ok := fetchersStatus[fetcher.Name()]; ok {
				c.Logger.Trace().
					Str("name", string(fetcher.Name())).
					Msg("Fetcher is already being processed or is processed, skipping.")

				continue
			}

			if !fetchersStatus.IsAllDone(fetcher.Dependencies()) {
				c.Logger.Trace().
					Str("name", string(fetcher.Name())).
					Msg("Fetcher's dependencies are not yet processed, skipping for now.")

				continue
			}

			fetchersToStart = append(fetchersToStart, fetcher)
		}

		c.Logger.Trace().
			Strs("names", fetchersToStart.GetNamesAsString()).
			Msg("Starting the following fetchers")

		wg.Add(len(fetchersToStart))

		for _, fetcher := range fetchersToStart {
			go processFetcher(fetcher)
		}

		wg.Wait()
	}

	return data, queries
}
