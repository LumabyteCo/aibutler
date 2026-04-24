package schedule

// ExportTimeNowUnixMilli returns the current timeNowUnixMilli function for saving/restoring in tests.
func ExportTimeNowUnixMilli() func() int64 {
	return timeNowUnixMilli
}

// SetTimeNowUnixMilli sets the timeNowUnixMilli function (for testing).
func SetTimeNowUnixMilli(fn func() int64) {
	timeNowUnixMilli = fn
}
