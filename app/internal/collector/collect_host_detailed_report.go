package collector

import "fmt"

// CollectHostDetailedReport builds a detailed report for a single host between timeFrom and timeTo.
func CollectHostDetailedReport(apiUrl, token, hostFilter string, timeFrom, timeTo int64, req ApiRequester) (HostDetailedReport, error) {
	focus, err := CollectHostFocusReport(apiUrl, token, hostFilter, timeFrom, timeTo, req, HostFocusOptions{})
	return focus.Detailed, err
}

// resampleSeries buckets points into [start..end] step seconds and returns timestamps and slice of *float64
func resampleSeries(points []map[string]interface{}, start, end, step int64) ([]int64, []*float64) {
	if step <= 0 {
		step = 60
	}
	n := int((end-start)/step) + 1
	timestamps := make([]int64, 0, n)
	for i := 0; int64(i) < int64(n); i++ {
		timestamps = append(timestamps, start+int64(i)*step)
	}
	sums := make([]float64, n)
	counts := make([]int, n)
	for _, p := range points {
		clk := parseInt(fmtValue(p["clock"]))
		if clk < start || clk > end {
			continue
		}
		idx := int((clk - start) / step)
		if idx < 0 || idx >= n {
			continue
		}
		vstr := fmtValue(p["value"])
		if vstr == "" {
			continue
		}
		var v float64
		_, _ = fmt.Sscanf(vstr, "%f", &v)
		sums[idx] += v
		counts[idx]++
	}
	values := make([]*float64, n)
	for i := 0; i < n; i++ {
		if counts[i] == 0 {
			values[i] = nil
			continue
		}
		avg := sums[i] / float64(counts[i])
		values[i] = &avg
	}
	return timestamps, values
}
