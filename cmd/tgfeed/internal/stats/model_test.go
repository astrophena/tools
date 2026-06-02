// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package stats

import (
	"testing"
	"time"

	"go.astrophena.name/base/testutil"
)

func TestRunFinalizeRequestTimingStats(t *testing.T) {
	t.Parallel()

	dns1 := 10 * time.Millisecond
	dns2 := 20 * time.Millisecond
	total1 := 100 * time.Millisecond
	total2 := 200 * time.Millisecond
	bodyRead := 50 * time.Millisecond

	run := &Run{}
	run.RecordRequestTimings(RequestTimingSample{
		DNS:              &dns1,
		ResponseBodyRead: &bodyRead,
		Total:            &total1,
	})
	run.RecordRequestTimings(RequestTimingSample{
		DNS:   &dns2,
		Total: &total2,
	})
	run.FinalizeRequestTimingStats()

	testutil.AssertEqual(t, run.RequestTiming.DNS.Count, 2)
	testutil.AssertEqual(t, run.RequestTiming.DNS.TotalMS, int64(30))
	testutil.AssertEqual(t, run.RequestTiming.DNS.AvgMS, int64(15))
	testutil.AssertEqual(t, run.RequestTiming.DNS.PercentileMS.P50, int64(10))
	testutil.AssertEqual(t, run.RequestTiming.DNS.PercentileMS.P90, int64(20))
	testutil.AssertEqual(t, run.RequestTiming.DNS.PercentileMS.Max, int64(20))

	testutil.AssertEqual(t, run.RequestTiming.ResponseBodyRead.Count, 1)
	testutil.AssertEqual(t, run.RequestTiming.ResponseBodyRead.TotalMS, int64(50))

	testutil.AssertEqual(t, run.RequestTiming.Total.Count, 2)
	testutil.AssertEqual(t, run.RequestTiming.Total.TotalMS, int64(300))
	testutil.AssertEqual(t, run.RequestTiming.Total.AvgMS, int64(150))
}
