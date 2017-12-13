package main

import (
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"strconv"
	"syscall"
	"time"
	"runtime"

	"github.com/gizak/termui"
)

const (
	// For inbound tests, we bump our timer by 2 seconds to account for
	// the remote server's test startup time
	DOWNLOAD_TEST_TIME = time.Second * time.Duration(30+2)
	UPLOAD_TEST_TIME = time.Second * time.Duration(30)
	REPORT_INTERVAL  = time.Millisecond * time.Duration(500)

	BLOCK_SIZE  = 200 * 1024 // size of each block of data copied to/from remote
)

type Throughput struct {
	sc *sparkyClient
	processBar *ProcessBar
	summary *Summary
	download *Download
	upload *Upload
}

func NewThroughput(sc *sparkyClient, processBar *ProcessBar) *Throughput {
	summary := newSummary()
	download := newDownload(sc, summary, processBar)
	upload := newUpload(sc, summary, processBar)
	return &Throughput {
		sc: sc,
		processBar: processBar,
		summary: summary,
		download: download,
		upload: upload,
	}
}

func (t *Throughput) run() {
	done := make(chan struct{})
	go t.summary.run(done)
	t.download.run()
	t.upload.run()
	close(done)
}

type Download struct {
	wr *widgetRenderer
	sc *sparkyClient
	summary *Summary
	processBar *ProcessBar
	blockTicker chan bool
}

func newDownload(sc *sparkyClient, summary *Summary, processBar *ProcessBar) *Download {
	// Build a download graph widget
	dlGraph := termui.NewLineChart()
	dlGraph.BorderLabel = " Download Speed (Mbit/s)"
	dlGraph.Data = []float64{0}
	dlGraph.Width = 30
	dlGraph.Height = 12
	dlGraph.PaddingTop = 1
	dlGraph.X = 0
	dlGraph.Y = 6
	// Windows Command Prompt doesn't support our Unicode characters with the default font
	if runtime.GOOS == "windows" {
		dlGraph.Mode = "dot"
		dlGraph.DotStyle = '+'
	}
	dlGraph.AxesColor = termui.ColorWhite
	dlGraph.LineColor = termui.ColorGreen | termui.AttrBold

	wr := newwidgetRenderer()
	wr.Add("dlgraph", dlGraph)
	wr.Render()

	return &Download {
		wr: wr,
		sc: sc,
		summary: summary,
		processBar: processBar,
		blockTicker: make(chan bool, 200),
	}
}

func (d *Download) run() {
	// Used to signal test completion to the throughput measurer
	done := make(chan struct{})

	// Launch a throughput measurer and then kick off the metered copy,
	// blocking until it completes.
	go d.downloadProcessor(done)
	d.downloadCopy(done)
}

func (d *Download) downloadProcessor(done <-chan struct{}) {
	var blockCount, prevBlockCount uint
	var tickCount uint
	var throughput float64
	var throughputHist []float64

	// reset progress bar
	d.processBar.update(0)

	tick := time.NewTicker(REPORT_INTERVAL)
	for {
		select {
		case <-d.blockTicker:
			// Increment our block counter when we get a ticker
			blockCount++

		case <-done:
			tick.Stop()
			return

		case <-tick.C:
			tickCount++
			amount := (blockCount - prevBlockCount) * BLOCK_SIZE * 8
			throughput = float64(amount)/1024/1024*float64(time.Second)/float64(REPORT_INTERVAL)

			// We discard the first element of the throughputHist slice once we have 70
			// elements stored.  This gives the user a chart that appears to scroll to
			// the left as new measurements come in and old ones are discarded.
			if len(throughputHist) >= 40 {
				throughputHist = throughputHist[1:]
			}

			// Add our latest measurement to the slice of historical measurements
			throughputHist = append(throughputHist, throughput)

			// Update the appropriate graph with the latest measurements
			d.wr.jobs["dlgraph"].(*termui.LineChart).Data = throughputHist
			d.wr.Render()

			// Send the latest measurement on to the stats generator
			d.summary.downloadRcv() <- SummaryData{amount, throughput}

			// Update the current block counter
			prevBlockCount = blockCount

			// update process bar
			d.processBar.update(uint(time.Duration(tickCount) * REPORT_INTERVAL * 100/DOWNLOAD_TEST_TIME))
		}
	}
}

func (d *Download) downloadCopy(done chan<- struct{}) {
	// Connect to the remote sparkyfish server
	d.sc.beginSession()
	defer d.sc.conn.Close()

	// Send the SND command to the remote server, requesting a download test
	// (remote sends).
	err := d.sc.writeCommand("SND")
	if err != nil {
		termui.Close()
		log.Fatalln(err)
	}

	// Set a timer for running the tests
	timer := time.NewTimer(DOWNLOAD_TEST_TIME)
	// Receive, tally, and discard incoming data as fast as we can until the sender stops sending or the timer expires
	for {
		select {
		case <-timer.C:
			// Timer has elapsed and test is finished
			close(done)
			return

		default:
			// Copy data from our net.Conn to the rubbish bin in (BLOCK_SIZE) KB chunks
			_, err := io.CopyN(ioutil.Discard, d.sc.conn, BLOCK_SIZE)
			if err != nil {
				// Handle the EOF when the test timer has expired at the remote end.
				if err == io.EOF || err == io.ErrClosedPipe || err == syscall.EPIPE {
					close(done)
					return
				}
				log.Println("Error copying:", err)
				return
			}
			// With each chunk copied, we send a message on our blockTicker channel
			d.blockTicker <- true
		}
	}
}


type Upload struct {
	wr *widgetRenderer
	sc *sparkyClient
	summary *Summary
	processBar *ProcessBar
	blockTicker chan bool
}

func newUpload(sc *sparkyClient, summary *Summary, processBar *ProcessBar) *Upload {
	// Build an upload graph widget
	ulGraph := termui.NewLineChart()
	ulGraph.BorderLabel = " Upload Speed (Mbit/s)"
	ulGraph.Data = []float64{0}
	ulGraph.Width = 30
	ulGraph.Height = 12
	ulGraph.PaddingTop = 1
	ulGraph.X = 30
	ulGraph.Y = 6
	// Windows Command Prompt doesn't support our Unicode characters with the default font
	if runtime.GOOS == "windows" {
		ulGraph.Mode = "dot"
		ulGraph.DotStyle = '+'
	}
	ulGraph.AxesColor = termui.ColorWhite
	ulGraph.LineColor = termui.ColorGreen | termui.AttrBold

	wr := newwidgetRenderer()
	wr.Add("ulgraph", ulGraph)
	wr.Render()

	return &Upload {
		wr: wr,
		sc: sc,
		summary: summary,
		processBar: processBar,
		blockTicker: make(chan bool, 200),
	}
}


func (u *Upload) run() {
	// Used to signal test completion to the throughput measurer
	done := make(chan struct{})

	// Launch a throughput measurer and then kick off the metered copy,
	// blocking until it completes.
	go u.uploadProcessor(done)
	u.uploadCopy(done)
}

func (u *Upload) uploadProcessor(done <-chan struct{}) {
	var blockCount, prevBlockCount, tickCount uint
	var throughput float64
	var throughputHist []float64

	// reset progress bar
	u.processBar.update(0)

	tick := time.NewTicker(REPORT_INTERVAL)
	for {
		select {
		case <-u.blockTicker:
			// Increment our block counter when we get a ticker
			blockCount++

		case <-done:
			tick.Stop()
			return

		case <-tick.C:
			tickCount++
			amount := (blockCount - prevBlockCount) * BLOCK_SIZE * 8
			throughput = float64(amount)/1024/1024*float64(time.Second)/float64(REPORT_INTERVAL)

			// We discard the first element of the throughputHist slice once we have 70
			// elements stored.  This gives the user a chart that appears to scroll to
			// the left as new measurements come in and old ones are discarded.
			if len(throughputHist) >= 40 {
				throughputHist = throughputHist[1:]
			}

			// Add our latest measurement to the slice of historical measurements
			throughputHist = append(throughputHist, throughput)

			// Update the appropriate graph with the latest measurements
			u.wr.jobs["ulgraph"].(*termui.LineChart).Data = throughputHist
			u.wr.Render()

			// Send the latest measurement on to the stats generator
			u.summary.uploadRcv() <- SummaryData{amount, throughput}

			// Update the current block counter
			prevBlockCount = blockCount

			// update process bar
			u.processBar.update(uint(time.Duration(tickCount) * REPORT_INTERVAL * 100/UPLOAD_TEST_TIME))
		}
	}
}


func (u *Upload) uploadCopy(done chan<- struct{}) {
	// Connect to the remote sparkyfish server
	u.sc.beginSession()
	defer u.sc.conn.Close()

	// Send the RCV command to the remote server, requesting an upload test
	// (remote receives).
	err := u.sc.writeCommand("RCV")
	if err != nil {
		termui.Close()
		log.Fatalln(err)
	}

	// Set a timer for running the tests
	timer := time.NewTimer(UPLOAD_TEST_TIME)

	// Send and tally outgoing data as fast as we can until the receiver stops receiving or the timer expires
	for {
		select {
		case <-timer.C:
			// Timer has elapsed and test is finished
			close(done)
			return

		default:
			// Copy data from our pre-filled bytes.Reader to the net.Conn in (BLOCK_SIZE) KB chunks
			_, err := io.CopyN(u.sc.conn, u.sc.randReader, BLOCK_SIZE)
			if err != nil {
				// If we get any of these errors, it probably just means that the server closed the connection
				if err == io.EOF || err == io.ErrClosedPipe || err == syscall.EPIPE {
					close(done)
					return
				}
				log.Println("Error copying:", err)
				return
			}

			// Make sure that we have enough runway in our bytes.Reader to handle the next read
			if u.sc.randReader.Len() <= BLOCK_SIZE {
				// We're nearing the end of the Reader, so seek back to the beginning and start again
				u.sc.randReader.Seek(0, 0)
			}

			// With each chunk copied, we send a message on our blockTicker channel
			u.blockTicker <- true
		}
	}
}


type Summary struct {
	wr *widgetRenderer
	downloadChan chan SummaryData
	uploadChan   chan SummaryData
}

type SummaryData struct {
	amount uint
	throughput float64
}

func newSummary() *Summary {
	// Build a stats summary widget
	statsSummary := termui.NewPar("")
	statsSummary.Height = 7
	statsSummary.Width = 60
	statsSummary.Y = 18
	statsSummary.BorderLabel = " Throughput Summary "
	statsSummary.Text = fmt.Sprintf("DOWNLOAD (Mbit/s)\nCurrent:--\t Max:--\t Avg:--\n\nUPLOAD (Mbit/s)\nCurrent:--\t Max:--\t Avg:--")
	statsSummary.TextFgColor = termui.ColorWhite | termui.AttrBold

	wr := newwidgetRenderer()
	wr.Add("statsSummary", statsSummary)
	wr.Render()

	return &Summary {
		wr: wr,
		downloadChan: make(chan SummaryData),
		uploadChan:   make(chan SummaryData),
	}
}

func (s *Summary) downloadRcv() chan<- SummaryData {
	return s.downloadChan
}

func (s *Summary) uploadRcv() chan<- SummaryData {
	return s.uploadChan
}

func (s *Summary) run(done <-chan struct{}) {
	var rcvData SummaryData
	var currentDL, maxDL, avgDL float64
	var currentUL, maxUL, avgUL float64
	var dlNum uint
	var dlAmount uint
	var ulNum uint
	var ulAmount uint

	for {
		select {
		case <-done:
			return

		case rcvData = <-s.downloadChan:
			currentDL = rcvData.throughput
			if currentDL > maxDL {
				maxDL = currentDL
			}
			dlNum++
			dlAmount += rcvData.amount
			avgDL = float64(dlAmount)/1024/1024*float64(time.Second)/float64(REPORT_INTERVAL)/float64(dlNum)

			// Update our stats widget with the latest readings
			s.wr.jobs["statsSummary"].(*termui.Par).Text = fmt.Sprintf("DOWNLOAD (Mbit/s) \nCurrent:%v\t Max:%v\t Avg:%v\n\nUPLOAD (Mbit/s)\nCurrent:%v\t Max:%v\t Avg:%v",
				strconv.FormatFloat(currentDL, 'f', 1, 64), strconv.FormatFloat(maxDL, 'f', 1, 64), strconv.FormatFloat(avgDL, 'f', 1, 64),
				strconv.FormatFloat(currentUL, 'f', 1, 64), strconv.FormatFloat(maxUL, 'f', 1, 64), strconv.FormatFloat(avgUL, 'f', 1, 64))
			s.wr.Render()

		case rcvData = <-s.uploadChan:
			currentUL = rcvData.throughput
			if currentUL > maxUL {
				maxUL = currentUL
			}
			ulNum++
			ulAmount += rcvData.amount
			avgUL = float64(ulAmount)/1024/1024*float64(time.Second)/float64(REPORT_INTERVAL)/float64(ulNum)

			// Update our stats widget with the latest readings
			s.wr.jobs["statsSummary"].(*termui.Par).Text = fmt.Sprintf("DOWNLOAD (Mbit/s) \nCurrent:%v\t Max:%v\t Avg:%v\n\nUPLOAD (Mbit/s)\nCurrent:%v\t Max:%v\t Avg:%v",
				strconv.FormatFloat(currentDL, 'f', 1, 64), strconv.FormatFloat(maxDL, 'f', 1, 64), strconv.FormatFloat(avgDL, 'f', 1, 64),
				strconv.FormatFloat(currentUL, 'f', 1, 64), strconv.FormatFloat(maxUL, 'f', 1, 64), strconv.FormatFloat(avgUL, 'f', 1, 64))
			s.wr.Render()
		}
	}
}

