// SPDX-License-Identifier: MIT
//
// Copyright © 2019 Kent Gibson <warthog618@gmail.com>.

// +build linux

package uapi_test

import (
	"fmt"
	"os"
	"testing"
	"time"
	"unsafe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/warthog618/gpiod/mockup"
	"github.com/warthog618/gpiod/uapi"
	"golang.org/x/sys/unix"
)

func TestRepeatedLines(t *testing.T) {
	t.Skip("leaves line as output as of 5.4-rc1")
	mockupRequired(t)
	c, err := mock.Chip(0)
	require.Nil(t, err)
	require.NotNil(t, c)
	f, err := os.Open(c.DevPath)
	require.Nil(t, err)

	hr := uapi.HandleRequest{
		Lines: 2,
	}
	hr.Offsets[0] = 1
	hr.Offsets[1] = 1

	// input
	err = uapi.GetLineHandle(f.Fd(), &hr)
	assert.NotNil(t, err)

	// output
	hr.Flags = uapi.HandleRequestOutput
	hr.DefaultValues[0] = 0
	hr.DefaultValues[1] = 1
	err = uapi.GetLineHandle(f.Fd(), &hr)
	assert.NotNil(t, err)

}

func TestAsIs(t *testing.T) {
	mockupRequired(t)
	c, err := mock.Chip(0)
	require.Nil(t, err)

	f, err := os.Open(c.DevPath)
	require.Nil(t, err)
	defer f.Close()

	hr := uapi.HandleRequest{
		Flags: uapi.HandleRequestInput,
		Lines: uint32(1),
	}
	copy(hr.Consumer[:31], "test-as-is")
	hr.Offsets[0] = uint32(3)
	err = uapi.GetLineHandle(f.Fd(), &hr)
	require.Nil(t, err)
	li, err := uapi.GetLineInfo(f.Fd(), 3)
	assert.Nil(t, err)
	xli := uapi.LineInfo{
		Offset: 3,
		Flags:  uapi.LineFlagRequested,
	}
	copy(xli.Name[:], li.Name[:]) // don't care about name
	copy(xli.Consumer[:31], "test-as-is")
	assert.Equal(t, xli, li)
	unix.Close(int(hr.Fd))

	li, err = uapi.GetLineInfo(f.Fd(), 3)
	assert.Nil(t, err)
	xli = uapi.LineInfo{
		Offset: 3,
		Flags:  0,
	}
	copy(xli.Name[:], li.Name[:]) // don't care about name
	assert.Equal(t, xli, li)

	hr.Flags = 0
	err = uapi.GetLineHandle(f.Fd(), &hr)
	require.Nil(t, err)
	li, err = uapi.GetLineInfo(f.Fd(), 3)
	assert.Nil(t, err)
	copy(xli.Consumer[:31], "test-as-is")
	xli.Flags = 1
	assert.Equal(t, xli, li)
	unix.Close(int(hr.Fd))
}

func TestWatchIsolation(t *testing.T) {
	t.Skip("fails on patch v1")
	mockupRequired(t)
	c, err := mock.Chip(0)
	require.Nil(t, err)

	f1, err := os.Open(c.DevPath)
	require.Nil(t, err)
	defer f1.Close()

	f2, err := os.Open(c.DevPath)
	require.Nil(t, err)
	defer f2.Close()

	// set watch
	li := uapi.LineInfo{Offset: 3}
	lname := c.Label + "-3"
	err = uapi.WatchLineInfo(f1.Fd(), &li)
	require.Nil(t, err)
	xli := uapi.LineInfo{Offset: 3}
	copy(xli.Name[:], lname)
	assert.Equal(t, xli, li)

	chg, err := readLineInfoChangedTimeout(f1.Fd(), time.Second)
	assert.Nil(t, err)
	assert.Nil(t, chg, "spurious change on f1")

	chg, err = readLineInfoChangedTimeout(f2.Fd(), time.Second)
	assert.Nil(t, err)
	assert.Nil(t, chg, "spurious change on f2")

	// request line
	start := time.Now()
	hr := uapi.HandleRequest{Lines: 1, Flags: uapi.HandleRequestInput}
	hr.Offsets[0] = 3
	copy(hr.Consumer[:], "testwatch")
	err = uapi.GetLineHandle(f2.Fd(), &hr)
	assert.Nil(t, err)
	chg, err = readLineInfoChangedTimeout(f1.Fd(), time.Second)
	assert.Nil(t, err)
	require.NotNil(t, chg)
	end := time.Now()
	assert.LessOrEqual(t, uint64(start.UnixNano()), chg.Timestamp)
	assert.GreaterOrEqual(t, uint64(end.UnixNano()), chg.Timestamp)
	assert.Equal(t, uapi.LineChangedRequested, chg.Type)
	xli.Flags |= uapi.LineFlagRequested
	copy(xli.Consumer[:], "testwatch")
	assert.Equal(t, xli, chg.Info)

	chg, err = readLineInfoChangedTimeout(f2.Fd(), time.Second)
	assert.Nil(t, err)
	assert.Nil(t, chg, "spurious change on f2")

	err = uapi.WatchLineInfo(f2.Fd(), &li)
	require.Nil(t, err)
	err = uapi.UnwatchLineInfo(f1.Fd(), li.Offset)
	require.Nil(t, err)
	unix.Close(int(hr.Fd))

	start = time.Now()
	unix.Close(int(hr.Fd))
	chg, err = readLineInfoChangedTimeout(f2.Fd(), time.Second)
	assert.Nil(t, err)
	require.NotNil(t, chg)
	end = time.Now()
	assert.LessOrEqual(t, uint64(start.UnixNano()), chg.Timestamp)
	assert.GreaterOrEqual(t, uint64(end.UnixNano()), chg.Timestamp)
	assert.Equal(t, uapi.LineChangedReleased, chg.Type)
	xli = uapi.LineInfo{Offset: 3}
	copy(xli.Name[:], lname)
	assert.Equal(t, xli, chg.Info)

	chg, err = readLineInfoChangedTimeout(f1.Fd(), time.Second)
	assert.Nil(t, err)
	assert.Nil(t, chg, "spurious change on f1")
}

func TestBulkEventRead(t *testing.T) {
	t.Skip("should return multiple events")
	mockupRequired(t)
	c, err := mock.Chip(0)
	require.Nil(t, err)
	f, err := os.Open(c.DevPath)
	require.Nil(t, err)
	defer f.Close()
	err = c.SetValue(1, 0)
	require.Nil(t, err)
	er := uapi.EventRequest{
		Offset: 1,
		HandleFlags: uapi.HandleRequestInput |
			uapi.HandleRequestActiveLow,
		EventFlags: uapi.EventRequestBothEdges,
	}
	err = uapi.GetLineEvent(f.Fd(), &er)
	require.Nil(t, err)

	evt, err := readEventTimeout(uintptr(er.Fd), time.Second)
	assert.Nil(t, err)
	assert.Nil(t, evt, "spurious event")

	c.SetValue(1, 1)
	c.SetValue(1, 0)
	c.SetValue(1, 1)
	c.SetValue(1, 0)

	var ed uapi.EventData
	b := make([]byte, unsafe.Sizeof(ed)*3)
	fmt.Printf("buffer size %d\n", len(b))
	n, err := unix.Read(int(er.Fd), b[:])
	assert.Nil(t, err)
	fmt.Printf("read %d\n", n)
	assert.Equal(t, len(b), n)

	unix.Close(int(er.Fd))
}

func TestOutputSets(t *testing.T) {
	t.Skip("contains known failures as of 5.4-rc1")
	mockupRequired(t)
	patterns := []struct {
		name string
		flag uapi.HandleFlag
	}{
		{"o", uapi.HandleRequestOutput},
		{"od", uapi.HandleRequestOutput | uapi.HandleRequestOpenDrain},
		{"os", uapi.HandleRequestOutput | uapi.HandleRequestOpenSource},
	}
	c, err := mock.Chip(0)
	require.Nil(t, err)
	line := 0
	for _, p := range patterns {
		for initial := 0; initial <= 1; initial++ {
			for toggle := 0; toggle <= 1; toggle++ {
				for activeLow := 0; activeLow <= 1; activeLow++ {
					final := initial
					if toggle == 1 {
						final ^= 0x01
					}
					flags := p.flag
					if activeLow == 1 {
						flags |= uapi.HandleRequestActiveLow
					}
					label := fmt.Sprintf("%s-%d-%d-%d(%d)", p.name, initial^1, initial, final, activeLow)
					tf := func(t *testing.T) {
						testLine(t, c, line, flags, initial, toggle)
					}
					t.Run(label, tf)
				}
			}
		}
	}
}

func testLine(t *testing.T, c *mockup.Chip, line int, flags uapi.HandleFlag, initial, toggle int) {
	t.Helper()
	// set mock initial - opposing default
	c.SetValue(line, initial^0x01)
	f, err := os.Open(c.DevPath)
	require.Nil(t, err)
	defer f.Close()
	// request line output
	hr := uapi.HandleRequest{
		Flags: flags,
		Lines: uint32(1),
	}
	hr.Offsets[0] = uint32(line)
	hr.DefaultValues[0] = uint8(initial)
	err = uapi.GetLineHandle(f.Fd(), &hr)
	require.Nil(t, err)
	if toggle != 0 {
		var hd uapi.HandleData
		hd[0] = uint8(initial ^ 0x01)
		err = uapi.SetLineValues(uintptr(hr.Fd), hd)
		assert.Nil(t, err, "can't set value 1")
		err = uapi.GetLineValues(uintptr(hr.Fd), &hd)
		assert.Nil(t, err, "can't get value 1")
		assert.Equal(t, uint8(initial^1), hd[0], "get value 1")
		hd[0] = uint8(initial)
		err = uapi.SetLineValues(uintptr(hr.Fd), hd)
		assert.Nil(t, err, "can't set value 2")
		err = uapi.GetLineValues(uintptr(hr.Fd), &hd)
		assert.Nil(t, err, "can't get value 2")
		assert.Equal(t, uint8(initial), hd[0], "get value 2")
		hd[0] = uint8(initial ^ 0x01)
		err = uapi.SetLineValues(uintptr(hr.Fd), hd)
		assert.Nil(t, err, "can't set value 3")
		err = uapi.GetLineValues(uintptr(hr.Fd), &hd)
		assert.Nil(t, err, "can't get value 3")
		assert.Equal(t, uint8(initial^1), hd[0], "get value 3")
	}
	// release
	unix.Close(int(hr.Fd))
}
