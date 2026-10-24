//go:build windows

package main

// Uploading animations to the Ajazz AK820 Pro's 128x128 screen. The sequence
// (START, IMAGE_CFG with the chunk count, 4096-byte chunks each acknowledged
// with 01 5A 02, then SAVE) and the data-pipe alignment step follow the
// firmware notes in ak820pro-linux (MIT, Copyright (c) 2024-2026 Ervin Urban).

import (
	"bytes"
	"errors"
	"fmt"
	"sync"
	"time"
)

const (
	ajazzDataUsagePage = 0xFF68
	ak820ChunkSize     = 4096
)

var ak820AckMagic = []byte{0x01, 0x5A, 0x02}

// screenUpload tracks the one upload that may run at a time.
var screenUpload struct {
	sync.Mutex
	running     bool
	done, total int
	result      string
}

type ak820Screen struct {
	ctrl, data *HidDev
}

func openAK820Screen(all []HidInfo) (*ak820Screen, error) {
	var ctrlInfo, dataInfo *HidInfo
	for i, h := range all {
		if !isAK820Pro(h) {
			continue
		}
		switch h.UsagePage {
		case ajazzUsagePage:
			ctrlInfo = &all[i]
		case ajazzDataUsagePage:
			dataInfo = &all[i]
		}
	}
	if ctrlInfo == nil || dataInfo == nil {
		return nil, errors.New("AK820 Pro screen interface not found (is the keyboard connected by cable or its receiver?)")
	}
	if dataInfo.OutLen != 65 && dataInfo.OutLen != ak820ChunkSize+1 {
		return nil, fmt.Errorf("unexpected screen report size %d", dataInfo.OutLen)
	}
	c, err := OpenHID(*ctrlInfo)
	if err != nil {
		return nil, err
	}
	d, err := OpenHID(*dataInfo)
	if err != nil {
		c.Close()
		return nil, err
	}
	return &ak820Screen{ctrl: c, data: d}, nil
}

func (s *ak820Screen) Close() { s.ctrl.Close(); s.data.Close() }

func (s *ak820Screen) feature(pkt []byte, wait time.Duration) error {
	if err := s.ctrl.SetFeature(append([]byte{0}, pkt...)); err != nil {
		return err
	}
	time.Sleep(wait)
	_, err := s.ctrl.GetFeature(0)
	return err
}

// writeData sends bytes to the data pipe in whatever report size Windows
// reports for it (64-byte packets or one 4096-byte chunk).
func (s *ak820Screen) writeData(b []byte) error {
	n := s.data.Info.OutLen - 1
	for off := 0; off < len(b); off += n {
		end := min(off+n, len(b))
		if err := s.data.Write(append([]byte{0}, b[off:end]...)); err != nil {
			return err
		}
	}
	return nil
}

// readAck waits for one chunk acknowledgement; nil means none arrived.
func (s *ak820Screen) readAck(timeout time.Duration) ([]byte, error) {
	r, err := s.data.Read(timeout)
	if err != nil || r == nil {
		return nil, err
	}
	if len(r) > 3 && r[0] == 0 && bytes.HasPrefix(r[1:], ak820AckMagic) {
		return r[1:], nil
	}
	if bytes.HasPrefix(r, ak820AckMagic) {
		return r, nil
	}
	return nil, fmt.Errorf("unexpected reply % x", r[:min(len(r), 4)])
}

func (s *ak820Screen) drain() {
	for i := 0; i < 64; i++ {
		if r, _ := s.data.Read(10 * time.Millisecond); r == nil {
			return
		}
	}
}

func imageCfg(chunks int) []byte {
	p := make([]byte, 64)
	p[0], p[1], p[2] = 0x04, 0x72, 0x03
	p[8], p[9] = byte(chunks), byte(chunks>>8)
	return p
}

// align resets the firmware's 64-packet counter inside a 4096-byte chunk, so a
// half-finished earlier transfer can't shift every frame. Only possible when
// the data pipe takes 64-byte reports.
func (s *ak820Screen) align() bool {
	if s.data.Info.OutLen != 65 {
		s.drain()
		return true
	}
	s.drain()
	if s.feature(ak820Cmd(0x18, false), 50*time.Millisecond) != nil {
		return false
	}
	if s.feature(imageCfg(3), 350*time.Millisecond) != nil {
		return false
	}
	s.drain()
	pkt := make([]byte, 64)
	coarse := 0
	for b := 1; b <= 8 && coarse == 0; b++ {
		for i := 0; i < 8; i++ {
			s.writeData(pkt)
		}
		if a, _ := s.readAck(220 * time.Millisecond); a != nil {
			coarse = b
		}
	}
	for i := 0; i < 56; i++ {
		s.writeData(pkt)
	}
	fine := 0
	for p := 1; p <= 8 && fine == 0; p++ {
		s.writeData(pkt)
		if a, _ := s.readAck(220 * time.Millisecond); a != nil {
			fine = p
		}
	}
	s.writeData(make([]byte, ak820ChunkSize))
	ack3, _ := s.readAck(time.Second)
	time.Sleep(100 * time.Millisecond)
	s.feature(ak820Cmd(0x02, false), 100*time.Millisecond)
	s.drain()
	return coarse > 0 && fine > 0 && ack3 != nil
}

// Upload streams a prepared payload (header + RGB565 frames, padded to whole
// chunks) and saves it. SAVE is only sent when every chunk was acknowledged,
// so a failed transfer never replaces what's on the screen with a partial one.
func (s *ak820Screen) Upload(payload []byte, progress func(done, total int)) error {
	if len(payload)%ak820ChunkSize != 0 {
		return errors.New("payload is not whole chunks")
	}
	if !s.align() && !s.align() {
		return errors.New("the screen did not respond to the alignment step; re-plug the keyboard and try again")
	}
	total := len(payload) / ak820ChunkSize
	if err := s.feature(ak820Cmd(0x18, false), 50*time.Millisecond); err != nil {
		return err
	}
	if err := s.feature(imageCfg(total), 350*time.Millisecond); err != nil {
		return err
	}
	s.drain()
	deadline := time.Now().Add(30*time.Second + time.Duration(total)*2*time.Second)
	for i := 0; i < total; i++ {
		if time.Now().After(deadline) {
			return fmt.Errorf("upload took too long (%d of %d chunks)", i, total)
		}
		if err := s.writeData(payload[i*ak820ChunkSize : (i+1)*ak820ChunkSize]); err != nil {
			return err
		}
		ack, err := s.readAck(time.Second)
		if err != nil {
			return err
		}
		if ack == nil {
			return fmt.Errorf("keyboard stopped acknowledging at chunk %d of %d", i+1, total)
		}
		if progress != nil {
			progress(i+1, total)
		}
	}
	time.Sleep(100 * time.Millisecond)
	if err := s.feature(ak820Cmd(0x02, false), 100*time.Millisecond); err != nil {
		return err
	}
	s.drain()
	return nil
}

// startScreenUpload renders the chosen animation and uploads it in the
// background; progress is visible through screenUpload.
func startScreenUpload(sc ScreenSettings) error {
	screenUpload.Lock()
	if screenUpload.running {
		screenUpload.Unlock()
		return errors.New("an upload is already running")
	}
	screenUpload.running, screenUpload.done, screenUpload.total, screenUpload.result = true, 0, 0, ""
	screenUpload.Unlock()
	go func() {
		err := func() error {
			payload, err := ScreenPayload(sc)
			if err != nil {
				return err
			}
			devMu.Lock() // keep the lighting loop off the keyboard meanwhile
			defer devMu.Unlock()
			all, err := EnumerateHID()
			if err != nil {
				return err
			}
			s, err := openAK820Screen(all)
			if err != nil {
				return err
			}
			defer s.Close()
			logf("AK820 Pro screen: uploading %s (%d bytes)", sc.Model, len(payload))
			return s.Upload(payload, func(d, t int) {
				screenUpload.Lock()
				screenUpload.done, screenUpload.total = d, t
				screenUpload.Unlock()
			})
		}()
		screenUpload.Lock()
		screenUpload.running = false
		if err != nil {
			screenUpload.result = "error: " + err.Error()
			logf("AK820 Pro screen: %v", err)
		} else {
			screenUpload.result = "ok"
			logf("AK820 Pro screen: upload complete")
		}
		screenUpload.Unlock()
	}()
	return nil
}
