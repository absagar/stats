package stats

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"runtime"
	"sync"
	"time"
)

type reformatURL func(string, string) string

type Stats struct {
	mu                sync.RWMutex
	Uptime            time.Time
	Pid               int
	ResponseCounts    map[string]int
	TotalResponseTime time.Time
	BadRoutes         map[string]int
	SlowRoutes        map[string]SlowRoutesData
	TimeoutRoutes     map[string]int
	Logger            *log.Logger
	Latency           time.Duration
	TimeoutLimit      time.Duration
	GetKey            reformatURL
}

type SlowRoutesData struct {
	Count   int
	AvgTime time.Duration
	MaxTime time.Duration
}

func New(logger *log.Logger, allowedLatency time.Duration, timeout time.Duration, fn reformatURL) *Stats {
	stats := &Stats{
		Uptime:            time.Now(),
		Pid:               os.Getpid(),
		ResponseCounts:    map[string]int{},
		TotalResponseTime: time.Time{},
		BadRoutes:         map[string]int{},
		SlowRoutes:        map[string]SlowRoutesData{},
		TimeoutRoutes:     map[string]int{},
		Logger:            logger,
		Latency:           allowedLatency,
		TimeoutLimit:      timeout,
	}
	//This function can be used to properly group the routes (for example - cases where variable parameters exist in URLs)
	if fn != nil {
		stats.GetKey = fn
	}

	return stats
}

// Negroni compatible interface
func (mw *Stats) ServeHTTP(w http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	if r.Method == "OPTIONS" {
		next(w, r)
		return
	}
	beginning, recorder := mw.Begin(w)
	currentRoute := mw.GetKey(r.URL.Path, r.Method)
	defer func(currentRoute string) {
		if err := recover(); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			stack := make([]byte, 1024*8)
			stack = stack[:runtime.Stack(stack, false)]
			mw.EndWithStatus(beginning, currentRoute, http.StatusInternalServerError)
			f := time.Now().UTC().String() + "PANIC: %s\n%s"
			mw.Logger.Printf(f, err, stack)
		} else {
			mw.EndWithStatus(beginning, currentRoute, recorder.Status())
		}
	}(currentRoute)
	next(recorder, r)
}

func (mw *Stats) Begin(w http.ResponseWriter) (time.Time, Recorder) {
	start := time.Now()

	writer := &RecorderResponseWriter{w, 200, 0}

	return start, writer
}

func (mw *Stats) EndWithStatus(start time.Time, currentRoute string, status int) {
	end := time.Now()

	responseTime := end.Sub(start)

	mw.mu.Lock()

	defer mw.mu.Unlock()

	statusCode := fmt.Sprintf("%d", status)

	mw.ResponseCounts[statusCode]++
	mw.TotalResponseTime = mw.TotalResponseTime.Add(responseTime)
	if status >= http.StatusInternalServerError {
		mw.BadRoutes[currentRoute] = mw.BadRoutes[currentRoute] + 1
	}
	if responseTime > mw.TimeoutLimit {
		mw.TimeoutRoutes[currentRoute] = mw.TimeoutRoutes[currentRoute] + 1
	} else if responseTime > mw.Latency {
		if _, ok := mw.SlowRoutes[currentRoute]; !ok {
			mw.SlowRoutes[currentRoute] = SlowRoutesData{Count: 1, AvgTime: responseTime, MaxTime: responseTime}
		} else {
			srd := mw.SlowRoutes[currentRoute]
			if responseTime > srd.MaxTime {
				srd.MaxTime = responseTime
			}
			srd.AvgTime = ((srd.AvgTime * time.Duration(srd.Count)) + responseTime) / (time.Duration(srd.Count + 1))
			srd.Count += 1
			mw.SlowRoutes[currentRoute] = srd
		}
	}
}

type data struct {
	Pid                    int                       `json:"pid"`
	UpTime                 string                    `json:"uptime"`
	UpTimeSec              float64                   `json:"uptime_sec"`
	Time                   string                    `json:"time"`
	TimeUnix               int64                     `json:"unixtime"`
	StatusCodeCount        map[string]int            `json:"status_code_count"`
	Count                  int                       `json:"count"`
	TotalResponseTime      string                    `json:"total_response_time"`
	TotalResponseTimeSec   float64                   `json:"total_response_time_sec"`
	AverageResponseTime    string                    `json:"average_response_time"`
	AverageResponseTimeSec float64                   `json:"average_response_time_sec"`
	BadRoutes              map[string]int            `json:"bad_routes"`
	SlowRoutes             map[string]SlowRoutesData `json:"slow_routes"`
	TimeoutRoutes          map[string]int            `json:timeout_routes`
}

func (mw *Stats) Data() *data {

	mw.mu.RLock()

	now := time.Now()

	uptime := now.Sub(mw.Uptime)

	count := 0
	for _, current := range mw.ResponseCounts {
		count += current
	}

	totalResponseTime := mw.TotalResponseTime.Sub(time.Time{})

	averageResponseTime := time.Duration(0)
	if count > 0 {
		avgNs := int64(totalResponseTime) / int64(count)
		averageResponseTime = time.Duration(avgNs)
	}

	r := &data{
		Pid:                    mw.Pid,
		UpTime:                 uptime.String(),
		UpTimeSec:              uptime.Seconds(),
		Time:                   now.String(),
		TimeUnix:               now.Unix(),
		StatusCodeCount:        mw.ResponseCounts,
		Count:                  count,
		TotalResponseTime:      totalResponseTime.String(),
		TotalResponseTimeSec:   totalResponseTime.Seconds(),
		AverageResponseTime:    averageResponseTime.String(),
		AverageResponseTimeSec: averageResponseTime.Seconds(),
		BadRoutes:              mw.BadRoutes,
		SlowRoutes:             mw.SlowRoutes,
		TimeoutRoutes:          mw.TimeoutRoutes,
	}

	mw.mu.RUnlock()

	return r
}
