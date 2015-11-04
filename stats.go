package stats

import (
	"log"
	"net/http"
	"os"
	"runtime"
	"sync"
	"time"
	"fmt"
)

type Stats struct {
	mu                  sync.RWMutex
	Uptime              time.Time
	Pid                 int
	ResponseCounts      map[string]int
	TotalResponseCounts map[string]int
	TotalResponseTime   time.Time
	BadRoutes           map[string]int
	Logger              *log.Logger
}

var currentRoute string

func New(logger *log.Logger) *Stats {
	stats := &Stats{
		Uptime:              time.Now(),
		Pid:                 os.Getpid(),
		ResponseCounts:      map[string]int{},
		TotalResponseCounts: map[string]int{},
		TotalResponseTime:   time.Time{},
		BadRoutes:           map[string]int{},
		Logger:              logger,
	}

	return stats
}

// Negroni compatible interface
func (mw *Stats) ServeHTTP(w http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	beginning, recorder := mw.Begin(w)
	currentRoute = r.URL.String() + ":" + r.Method
	defer func() {
		if err := recover(); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			stack := make([]byte, 1024*8)
			stack = stack[:runtime.Stack(stack, false)]
			mw.EndWithStatus(beginning, http.StatusInternalServerError)
			f := "PANIC: %s\n%s"
			mw.Logger.Printf(f, err, stack)
		} else {
			mw.EndWithStatus(beginning, recorder.Status())
		}
	}()
	next(recorder, r)
}

func (mw *Stats) Begin(w http.ResponseWriter) (time.Time, Recorder) {
	start := time.Now()

	writer := &RecorderResponseWriter{w, 200, 0}

	return start, writer
}

func (mw *Stats) EndWithStatus(start time.Time, status int) {
	end := time.Now()

	responseTime := end.Sub(start)

	mw.mu.Lock()

	defer mw.mu.Unlock()

	statusCode := fmt.Sprintf("%d", status)

	mw.ResponseCounts[statusCode]++
	mw.TotalResponseCounts[statusCode]++
	mw.TotalResponseTime = mw.TotalResponseTime.Add(responseTime)
	if status >= http.StatusInternalServerError {
		mw.BadRoutes[currentRoute] = mw.BadRoutes[currentRoute] + 1
	}
}

type data struct {
	Pid                    int            `json:"pid"`
	UpTime                 string         `json:"uptime"`
	UpTimeSec              float64        `json:"uptime_sec"`
	Time                   string         `json:"time"`
	TimeUnix               int64          `json:"unixtime"`
	StatusCodeCount        map[string]int `json:"status_code_count"`
	TotalStatusCodeCount   map[string]int `json:"total_status_code_count"`
	Count                  int            `json:"count"`
	TotalCount             int            `json:"total_count"`
	TotalResponseTime      string         `json:"total_response_time"`
	TotalResponseTimeSec   float64        `json:"total_response_time_sec"`
	AverageResponseTime    string         `json:"average_response_time"`
	AverageResponseTimeSec float64        `json:"average_response_time_sec"`
	BadRoutes              map[string]int `json:bad_routes`
}

func (mw *Stats) Data() *data {

	mw.mu.RLock()

	now := time.Now()

	uptime := now.Sub(mw.Uptime)

	count := 0
	for _, current := range mw.ResponseCounts {
		count += current
	}

	totalCount := 0
	for _, count := range mw.TotalResponseCounts {
		totalCount += count
	}

	totalResponseTime := mw.TotalResponseTime.Sub(time.Time{})

	averageResponseTime := time.Duration(0)
	if totalCount > 0 {
		avgNs := int64(totalResponseTime) / int64(totalCount)
		averageResponseTime = time.Duration(avgNs)
	}

	r := &data{
		Pid:                    mw.Pid,
		UpTime:                 uptime.String(),
		UpTimeSec:              uptime.Seconds(),
		Time:                   now.String(),
		TimeUnix:               now.Unix(),
		StatusCodeCount:        mw.ResponseCounts,
		TotalStatusCodeCount:   mw.TotalResponseCounts,
		Count:                  count,
		TotalCount:             totalCount,
		TotalResponseTime:      totalResponseTime.String(),
		TotalResponseTimeSec:   totalResponseTime.Seconds(),
		AverageResponseTime:    averageResponseTime.String(),
		AverageResponseTimeSec: averageResponseTime.Seconds(),
		BadRoutes:              mw.BadRoutes,
	}

	mw.mu.RUnlock()

	return r
}
