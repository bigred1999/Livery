//go:build windows

package main

import (
	"math"
	"sync"
	"time"
)

var orgbAnim struct {
	sync.Mutex
	stop chan struct{}
}

func stopOpenRGBBreathe() {
	orgbAnim.Lock()
	if orgbAnim.stop != nil {
		close(orgbAnim.stop)
		orgbAnim.stop = nil
	}
	orgbAnim.Unlock()
}

func startOpenRGBBreathe(t ThemeDef, s Settings) {
	stopOpenRGBBreathe()
	stop := make(chan struct{})
	orgbAnim.Lock()
	orgbAnim.stop = stop
	orgbAnim.Unlock()
	go func() {
		start := time.Now()
		period := breathPeriod(s.Speed)
		for {
			o, err := ConnectOpenRGB()
			if err == nil {
				ctrls, err := o.Controllers()
				var targets []orgbController
				if err == nil {
					for _, c := range ctrls {
						if len(c.LEDNames) > 0 && !handledNatively(c.Location) {
							o.send(c.Index, orgbSetCustomMode, nil)
							targets = append(targets, c)
						}
					}
				}
				tick := time.NewTicker(50 * time.Millisecond)
			anim:
				for len(targets) > 0 {
					select {
					case <-stop:
						tick.Stop()
						o.Close()
						return
					case <-tick.C:
						f := breathLevel(time.Since(start), period)
						for _, c := range targets {
							if err := o.UpdateLEDs(c.Index, c.ColorsFor(t, int(math.Round(float64(s.Brightness)*f)))); err != nil {
								break anim // connection lost: reconnect
							}
						}
					}
				}
				tick.Stop()
				o.Close()
			}
			select {
			case <-stop:
				return
			case <-time.After(10 * time.Second):
			}
		}
	}()
}
