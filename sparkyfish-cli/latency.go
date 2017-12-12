package main

import (
	"fmt"
	"log"
	"math"
	"sort"
	"time"

	"github.com/gizak/termui"
)


type Latency struct {
	sc *sparkyClient
	wr *widgetRenderer
	processBar *ProcessBar
	pingTime chan time.Duration
}

func NewLatency(sc *sparkyClient, processBar *ProcessBar) *Latency {
	latencyGraph := termui.NewSparkline()
	latencyGraph.LineColor = termui.ColorCyan
	latencyGraph.Height = 3

	latencyGroup := termui.NewSparklines(latencyGraph)
	latencyGroup.Y = 3
	latencyGroup.Height = 3
	latencyGroup.Width = 30
	latencyGroup.Border = false
	latencyGroup.Lines[0].Data = []int{0}

	latencyTitle := termui.NewPar("Latency")
	latencyTitle.Height = 1
	latencyTitle.Width = 30
	latencyTitle.Border = false
	latencyTitle.TextFgColor = termui.ColorGreen
	latencyTitle.Y = 2

	latencyStats := termui.NewPar("")
	latencyStats.Height = 4
	latencyStats.Width = 30
	latencyStats.X = 32
	latencyStats.Y = 2
	latencyStats.Border = false
	latencyStats.TextFgColor = termui.ColorWhite | termui.AttrBold
	latencyStats.Text = "Last: 30ms\nMin: 2ms\nMax: 34ms"

	wr := newwidgetRenderer()
	wr.Add("latency", latencyGroup)
	wr.Add("latencytitle", latencyTitle)
	wr.Add("latencystats", latencyStats)
	wr.Render()

	return &Latency {
		sc: sc,
		wr: wr,
		processBar: processBar,
		pingTime: make(chan time.Duration, 10),
	}
}

func (l *Latency) run() {
	// Reset our progress bar to 0% if it's not there already
	//l.progressBarReset <- true
	l.processBar.update(0)

	processorReady := make(chan struct{})
	doneProcessor := make(chan struct{})
	// start our ping processor
	go l.latencyProcessor(processorReady, doneProcessor)

	// Wait for our processor to become ready
	<-processorReady

	buf := make([]byte, 1)

	l.sc.beginSession()
	defer l.sc.conn.Close()

	// Send the ECO command to the remote server, requesting an echo test
	// (remote receives and echoes back).
	err := l.sc.writeCommand("ECO")
	if err != nil {
		termui.Close()
		log.Fatalln(err)
	}

	for c := 0; c <= numPings-1; c++ {
		startTime := time.Now()
		l.sc.conn.Write([]byte{byte(c)})

		_, err = l.sc.conn.Read(buf)
		if err != nil {
			log.Fatal(err)
		}
		endTime := time.Now()

		l.pingTime <- endTime.Sub(startTime)
	}

	close(doneProcessor)

	// Kill off the progress bar updater and block until it's gone
	l.processBar.update(100)

	return
}

func (l *Latency) latencyProcessor(ready chan<- struct{}, done <-chan struct{}) {
	var ptMax, ptMin int
	var latencyHist pingHistory

	// Signal pingTest() that we're ready
	close(ready)

	for {
		select {
		//case <-timeout.C:
		case <-done:
			// If we've been pinging for maxPingTestLength, call it quits
			return
		case pt := <-l.pingTime:
			// Calculate our ping time in microseconds
			ptMicro := pt.Nanoseconds() / 1000

			// Add this ping to our ping history
			latencyHist = append(latencyHist, ptMicro)

			ptMin, ptMax = latencyHist.minMax()

			// update the progress bar
			l.processBar.update(uint(len(latencyHist)*100/numPings))

			// Update the ping stats widget
			l.wr.jobs["latency"].(*termui.Sparklines).Lines[0].Data = latencyHist.toMilli()
			l.wr.jobs["latencystats"].(*termui.Par).Text = fmt.Sprintf("Cur/Min/Max\n%.2f/%.2f/%.2f ms\nAvg/σ\n%.2f/%.2f ms",
				float64(ptMicro/1000), float64(ptMin/1000), float64(ptMax/1000), latencyHist.mean()/1000, latencyHist.stdDev()/1000)
			l.wr.Render()
		}
	}
}

type pingHistory []int64

// toMilli Converts our ping history to milliseconds for display purposes
func (h *pingHistory) toMilli() []int {
	var pingMilli []int

	for _, v := range *h {
		pingMilli = append(pingMilli, int(v/1000))
	}

	return pingMilli
}

// mean generates a statistical mean of our historical ping times
func (h *pingHistory) mean() float64 {
	var sum uint64
	for _, t := range *h {
		sum = sum + uint64(t)
	}

	return float64(sum / uint64(len(*h)))
}

// variance calculates the variance of our historical ping times
func (h *pingHistory) variance() float64 {
	var sqDevSum float64

	mean := h.mean()

	for _, t := range *h {
		sqDevSum = sqDevSum + math.Pow((float64(t)-mean), 2)
	}
	return sqDevSum / float64(len(*h))
}

// stdDev calculates the standard deviation of our historical ping times
func (h *pingHistory) stdDev() float64 {
	return math.Sqrt(h.variance())
}

func (h *pingHistory) minMax() (int, int) {
	var hist []int
	for _, v := range *h {
		hist = append(hist, int(v))
	}
	sort.Ints(hist)
	return hist[0], hist[len(hist)-1]
}
