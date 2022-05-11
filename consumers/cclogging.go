/*
 * Created by dengshiwei on 2020/01/06.
 * Copyright 2015－2020 Sensors Data Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *       http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package consumers

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"syscall"
	"time"

	"github.com/tonghs/sa-sdk-go/structs"
)

type ConcurrentLoggingConsumer struct {
	w     *ConcurrentLogWriter
	Fname string
	Hour  bool
}

func InitConcurrentLoggingConsumer(fname string, hour bool) (*ConcurrentLoggingConsumer, error) {
	w, err := InitConcurrentLogWriter(fname, hour)
	if err != nil {
		return nil, err
	}

	c := &ConcurrentLoggingConsumer{Fname: fname, Hour: hour, w: w}
	return c, nil
}

func (c *ConcurrentLoggingConsumer) Send(data structs.EventData) error {
	return c.w.Write(data)
}

func (c *ConcurrentLoggingConsumer) Flush() error {
	c.w.Flush()
	return nil
}

func (c *ConcurrentLoggingConsumer) Close() error {
	c.w.Close()
	return nil
}

func (c *ConcurrentLoggingConsumer) ItemSend(item structs.Item) error {
	return c.w.writeItem(item)
}

type ConcurrentLogWriter struct {
	rec chan string

	fname string
	file  *os.File

	day  int
	hour int

	hourRotate bool

	wg sync.WaitGroup
}

func (w *ConcurrentLogWriter) Write(data structs.EventData) error {
	bdata, err := json.Marshal(data)
	if err != nil {
		return err
	}

	w.rec <- string(bdata)
	return nil
}

func (w *ConcurrentLogWriter) writeItem(item structs.Item) error {
	itemData, err := json.Marshal(item)
	if err != nil {
		return nil
	}

	w.rec <- string(itemData)
	return nil
}

func (w *ConcurrentLogWriter) Flush() {
	w.file.Sync()
}

func (w *ConcurrentLogWriter) Close() {
	close(w.rec)
	w.wg.Wait()
}

func (w *ConcurrentLogWriter) intRotate() error {
	fname := ""

	if w.file != nil {
		w.file.Close()
	}

	now := time.Now()
	today := now.Format("2006-01-02")
	w.day = time.Now().Day()

	if w.hourRotate {
		hour := now.Hour()
		w.hour = hour

		fname = fmt.Sprintf("%s.%s.%02d", w.fname, today, hour)
	} else {
		fname = fmt.Sprintf("%s.%s", w.fname, today)
	}

	fd, err := os.OpenFile(fname, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0644)
	if err != nil {
		fmt.Printf("open failed: %s\n", err)
		return err
	}
	w.file = fd

	return nil
}

func InitConcurrentLogWriter(fname string, hourRotate bool) (*ConcurrentLogWriter, error) {
	w := &ConcurrentLogWriter{
		fname:      fname,
		day:        time.Now().Day(),
		hour:       time.Now().Hour(),
		hourRotate: hourRotate,
		rec:        make(chan string, CHANNEL_SIZE),
	}

	if err := w.intRotate(); err != nil {
		fmt.Fprintf(os.Stderr, "ConcurrentLogWriter(%q): %s\n", w.fname, err)
		return nil, err
	}

	w.wg.Add(1)

	go func() {
		defer func() {
			if w.file != nil {
				w.file.Sync()
				w.file.Close()
			}
			w.wg.Done()
		}()

		for {
			select {
			case rec, ok := <-w.rec:
				if !ok {
					return
				}

				now := time.Now()

				if (w.hourRotate && now.Hour() != w.hour) ||
					(now.Day() != w.day) {
					if err := w.intRotate(); err != nil {
						fmt.Fprintf(os.Stderr, "ConcurrentLogWriter(%q): %s\n", w.fname, err)
						return
					}
				}

				syscall.Flock(int(w.file.Fd()), syscall.LOCK_EX)
				_, err := fmt.Fprintln(w.file, rec)
				if err != nil {
					fmt.Fprintf(os.Stderr, "ConcurrentLogWriter(%q): %s\n", w.fname, err)
					syscall.Flock(int(w.file.Fd()), syscall.LOCK_UN)
					return
				}
				syscall.Flock(int(w.file.Fd()), syscall.LOCK_UN)
			}
		}
	}()

	return w, nil
}
