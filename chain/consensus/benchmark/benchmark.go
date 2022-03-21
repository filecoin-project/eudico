package benchmark

import (
	"context"
	"fmt"
	"math"
	"time"

	lapi "github.com/filecoin-project/lotus/api"
)

func RunSimpleBenchmark(ctx context.Context, api lapi.FullNode, benchmarkLength int) (TestnetStats, error) {
	var crossMsgsNum, blsMsgsNum, secpkMsgsNum, h int
	var changes []*lapi.HeadChange
	var blockTimes []time.Time

	chain, err := api.ChainNotify(ctx)
	if err != nil {
		return TestnetStats{}, err
	}

	calculateMessagesNum := func(changes []*lapi.HeadChange) error {
		for _, change := range changes {
			for _, block := range change.Val.Blocks() {
				msgs, err := api.ChainGetBlockMessages(ctx, block.Cid())
				if err != nil {
					return err
				}
				crossMsgsNum += len(msgs.CrossMessages)
				blsMsgsNum += len(msgs.BlsMessages)
				secpkMsgsNum += len(msgs.SecpkMessages)
			}
		}
		return nil
	}

	startAt := time.Now()
	for h < benchmarkLength {
		select {
		case <-ctx.Done():
			return TestnetStats{}, fmt.Errorf("benchmark was canceled")
		case change := <-chain:
			blockTimes = append(blockTimes, time.Now())
			changes = append(changes, change...)
			h++
		}
	}
	duration := time.Since(startAt)

	err = calculateMessagesNum(changes)
	if err != nil {
		return TestnetStats{}, err
	}

	ts := NewTestnetStats(
		blockTimes,
		duration,
		benchmarkLength,
		crossMsgsNum,
		blsMsgsNum,
		secpkMsgsNum,
		int64(changes[0].Val.Height()),
		int64(changes[len(changes)-1].Val.Height()),
	)
	return ts, nil
}

type TestnetStats struct {
	startHeight     int64
	endHeight       int64
	totalTime       time.Duration
	benchmarkLength int

	// Messages
	blsMessagesNum   int
	crossMessagesNum int
	secpkMessagesNum int

	// average time to produce a block
	mean time.Duration
	// standard deviation of block production
	std float64
	// longest time to produce a block
	max time.Duration
	// shortest time to produce a block
	min time.Duration
	// Block Throughput
	bTpt float64
	// Message Throughput
	mTpt float64
}

func NewTestnetStats(
	blockTimes []time.Time,
	duration time.Duration,
	benchmarkLength int,
	crossMsgsNum int,
	blsMsgsNum int,
	secpkMsgsNum int,
	startHeight int64,
	endHeight int64,
) TestnetStats {
	timeIntervals := splitIntoBlockIntervals(blockTimes)
	testnetStats := extractTestnetStats(timeIntervals)

	testnetStats.totalTime = duration
	testnetStats.benchmarkLength = benchmarkLength
	testnetStats.startHeight = startHeight
	testnetStats.endHeight = endHeight

	testnetStats.populateMessages(blsMsgsNum, crossMsgsNum, secpkMsgsNum)
	testnetStats.computeThroughput()

	return testnetStats
}

func (t *TestnetStats) populateMessages(b, c, s int) {
	t.blsMessagesNum = b
	t.crossMessagesNum = c
	t.secpkMessagesNum = s
}

func (t *TestnetStats) computeThroughput() {
	t.bTpt = float64(t.benchmarkLength) / t.totalTime.Seconds()
	t.mTpt = float64(t.secpkMessagesNum+t.blsMessagesNum+t.crossMessagesNum) / t.totalTime.Seconds()
}

func (t *TestnetStats) String() string {
	return fmt.Sprintf(`Benchmarked from height %v to %v
	Mean Block Interval: %v
	Standard Deviation: %f
	Max Block Interval: %v
	Min Block Interval: %v
	Duration: %v
	BLS Message number: %v,
	Cross Messages number: %v,
	Secpk Messages number: %v,
	Block throughput: %v bps,
	Message throughput: %v tps,
	`,
		t.startHeight,
		t.endHeight,
		t.mean,
		t.std,
		t.max,
		t.min,
		t.totalTime,
		t.blsMessagesNum,
		t.crossMessagesNum,
		t.secpkMessagesNum,
		t.bTpt,
		t.mTpt,
	)
}

func extractTestnetStats(intervals []time.Duration) TestnetStats {
	var (
		sum, mean time.Duration
		std       float64
		max       = intervals[0]
		min       = intervals[0]
	)

	for _, interval := range intervals {
		sum += interval

		if interval > max {
			max = interval
		}

		if interval < min {
			min = interval
		}
	}
	mean = sum / time.Duration(len(intervals))

	for _, interval := range intervals {
		diff := (interval - mean).Seconds()
		std += math.Pow(diff, 2)
	}
	std = math.Sqrt(std / float64(len(intervals)))

	return TestnetStats{
		mean: mean,
		std:  std,
		max:  max,
		min:  min,
	}
}

func splitIntoBlockIntervals(blockTimes []time.Time) []time.Duration {
	intervals := make([]time.Duration, len(blockTimes)-1)
	lastTime := blockTimes[0]
	for i, t := range blockTimes {
		// skip the first block
		if i == 0 {
			continue
		}

		intervals[i-1] = t.Sub(lastTime)
		lastTime = t
	}
	return intervals
}
