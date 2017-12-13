package main

import (
	"bufio"
	"bytes"
	"fmt"
	"log"
	"net"
	"os"

	"github.com/dustin/randbo"
	"github.com/gizak/termui"
)

type sparkyClient struct {
	conn               net.Conn
	reader             *bufio.Reader
	randomData         []byte
	randReader         *bytes.Reader
	serverCname        string
	serverLocation     string
	serverHostname     string
	titleBanner        *TitleBanner
	processBar         *ProcessBar
	latency            *Latency
	throughput         *Throughput
	help               *Help
}

func main() {
	if len(os.Args) < 2 {
		log.Fatal("Usage: ", os.Args[0], " <sparkyfish server hostname/IP>[:port]")
	}

	dest := os.Args[1]
	i := last(dest, ':')
	if i < 0 {
		dest = fmt.Sprint(dest, ":7121")
	}

	// Initialize our screen
	err := termui.Init()
	if err != nil {
		panic(err)
	}

	if termui.TermWidth() < 60 || termui.TermHeight() < 28 {
		fmt.Println("sparkyfish needs a terminal window at least 60x28 to run.")
		os.Exit(1)
	}

	defer termui.Close()

	// 'q' quits the program
	termui.Handle("/sys/kbd/q", func(termui.Event) {
		termui.StopLoop()
	})
	// 'Q' also works
	termui.Handle("/sys/kbd/Q", func(termui.Event) {
		termui.StopLoop()
	})

	sc := newsparkyClient()
	sc.serverHostname = dest

	// Begin our tests
	go sc.runTestSequence()

	termui.Loop()
}

// NewsparkyClient creates a new sparkyClient object
func newsparkyClient() *sparkyClient {
	m := sparkyClient{}

	// Make a 10MB byte slice to hold our random data blob
	m.randomData = make([]byte, 1024*1024*10)

	// Use a randbo Reader to fill our big slice with random data
	_, err := randbo.New().Read(m.randomData)
	if err != nil {
		log.Fatalln("error generating random data:", err)
	}

	// Create a bytes.Reader over this byte slice
	m.randReader = bytes.NewReader(m.randomData)
	m.titleBanner = NewTileBanner()
	m.titleBanner.setTile("Please contact +8618675507109 after failure.")
	m.processBar = NewProcessBar()
	m.latency = NewLatency(&m, m.processBar)
	m.throughput = NewThroughput(&m, m.processBar)
	m.help = NewHelp()

	return &m
}

func (sc *sparkyClient) runTestSequence() {
	// Start our ping test and block until it's complete
	sc.latency.run()

	sc.throughput.run()

	sc.processBar.finish()
}

type ProcessBar struct {
	wr *widgetRenderer
	curPercent uint
}

func NewProcessBar() *ProcessBar {
	wr := newwidgetRenderer()

	// Build out progress gauge widget
	progress := termui.NewGauge()
	progress.Percent = 40
	progress.Width = 60
	progress.Height = 3
	progress.Y = 25
	progress.X = 0
	progress.Border = true
	progress.BorderLabel = " Test Progress "
	progress.Percent = 0
	progress.BarColor = termui.ColorRed
	progress.BorderFg = termui.ColorWhite
	progress.PercentColorHighlighted = termui.ColorWhite | termui.AttrBold
	progress.PercentColor = termui.ColorWhite | termui.AttrBold

	wr.Add("progress", progress)
	wr.Render()

	return &ProcessBar{
		wr: wr,
	}
}

func (bar *ProcessBar) update(percent uint) {
	if percent > 100 {
		percent = 100
	}
	bar.curPercent = percent
	bar.wr.jobs["progress"].(*termui.Gauge).BarColor = termui.ColorRed
	bar.wr.jobs["progress"].(*termui.Gauge).Percent = int(bar.curPercent)
	bar.wr.Render()
}

func (bar *ProcessBar) forward(percent uint) {
	bar.curPercent += percent
	if bar.curPercent > 100 {
		bar.curPercent = 100
	}
	bar.wr.jobs["progress"].(*termui.Gauge).BarColor = termui.ColorRed
	bar.wr.jobs["progress"].(*termui.Gauge).Percent = int(bar.curPercent)
	bar.wr.Render()
}

func (bar *ProcessBar) finish() {
	bar.curPercent = 100
	bar.wr.jobs["progress"].(*termui.Gauge).BarColor = termui.ColorGreen
	bar.wr.jobs["progress"].(*termui.Gauge).Percent = int(bar.curPercent)
	bar.wr.Render()
}

func fatalError(err error) {
	termui.Clear()
	termui.Close()
	log.Fatal(err)
}

// Index of rightmost occurrence of b in s.
// Borrowed from golang.org/pkg/net/net.go
func last(s string, b byte) int {
	i := len(s)
	for i--; i >= 0; i-- {
		if s[i] == b {
			break
		}
	}
	return i
}


type TitleBanner struct {
	wr *widgetRenderer
}

func NewTileBanner() *TitleBanner {
	// Build our title box
	titleBox := termui.NewPar("")
	titleBox.Height = 1
	titleBox.Width = 60
	titleBox.Y = 0
	titleBox.Border = false
	titleBox.TextFgColor = termui.ColorWhite | termui.AttrBold

	// Build the server name/location banner line
	bannerBox := termui.NewPar("")
	bannerBox.Height = 1
	bannerBox.Width = 60
	bannerBox.Y = 1
	bannerBox.Border = false
	bannerBox.TextFgColor = termui.ColorRed | termui.AttrBold

	wr := newwidgetRenderer()
	wr.Add("titlebox", titleBox)
	wr.Add("bannerbox", bannerBox)
	wr.Render()

	return &TitleBanner {
		wr: wr,
	}
}

func (tb *TitleBanner) setTile(title string) {
	if len(title) > 60 {
		title = title[:59]
	}
	tb.wr.jobs["titlebox"].(*termui.Par).Text = title
	tb.wr.Render()
}

func (tb *TitleBanner) setBanner(banner string) {
	if len(banner) > 60 {
		banner = banner[:59]
	}
	tb.wr.jobs["bannerbox"].(*termui.Par).Text = banner
	tb.wr.Render()
}

type Help struct {
	wr *widgetRenderer
}

func NewHelp() *Help {
	// Build our helpbox widget
	helpBox := termui.NewPar(" COMMANDS: [q]uit")
	helpBox.Height = 1
	helpBox.Width = 60
	helpBox.Y = 28
	helpBox.Border = false
	helpBox.TextBgColor = termui.ColorBlue
	helpBox.TextFgColor = termui.ColorYellow | termui.AttrBold
	helpBox.Bg = termui.ColorBlue

	wr := newwidgetRenderer()
	wr.Add("helpbox", helpBox)
	wr.Render()

	return &Help {
		wr: wr,
	}
}
