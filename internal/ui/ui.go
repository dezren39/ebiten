// Copyright 2022 The Ebiten Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package ui

import (
	"errors"
	"image"
	"sync"
	"sync/atomic"

	"github.com/hajimehoshi/ebiten/v2/internal/atlas"
	"github.com/hajimehoshi/ebiten/v2/internal/mipmap"
	"github.com/hajimehoshi/ebiten/v2/internal/thread"
)

// RegularTermination represents a regular termination.
// Run can return this error, and if this error is received,
// the game loop should be terminated as soon as possible.
var RegularTermination = errors.New("regular termination")

type FPSModeType int

const (
	FPSModeVsyncOn FPSModeType = iota
	FPSModeVsyncOffMaximum
	FPSModeVsyncOffMinimum
)

type CursorMode int

const (
	CursorModeVisible CursorMode = iota
	CursorModeHidden
	CursorModeCaptured
)

type CursorShape int

const (
	CursorShapeDefault CursorShape = iota
	CursorShapeText
	CursorShapeCrosshair
	CursorShapePointer
	CursorShapeEWResize
	CursorShapeNSResize
	CursorShapeNESWResize
	CursorShapeNWSEResize
	CursorShapeMove
	CursorShapeNotAllowed
)

type WindowResizingMode int

const (
	WindowResizingModeDisabled WindowResizingMode = iota
	WindowResizingModeOnlyFullscreenEnabled
	WindowResizingModeEnabled
)

type UserInterface struct {
	err  error
	errM sync.Mutex

	isScreenClearedEveryFrame int32
	graphicsLibrary           int32
	running                   int32
	terminated                int32

	whiteImage *Image

	mainThread thread.Thread

	userInterfaceImpl
}

var (
	theUI *UserInterface
)

func init() {
	// newUserInterface() must be called in the main goroutine.
	u, err := newUserInterface()
	if err != nil {
		panic(err)
	}
	theUI = u
}

func Get() *UserInterface {
	return theUI
}

// newUserInterface must be called from the main thread.
func newUserInterface() (*UserInterface, error) {
	u := &UserInterface{
		isScreenClearedEveryFrame: 1,
		graphicsLibrary:           int32(GraphicsLibraryUnknown),
	}

	u.whiteImage = u.NewImage(3, 3, atlas.ImageTypeRegular)
	pix := make([]byte, 4*u.whiteImage.width*u.whiteImage.height)
	for i := range pix {
		pix[i] = 0xff
	}
	// As a white image is used at Fill, use WritePixels instead.
	u.whiteImage.WritePixels(pix, image.Rect(0, 0, u.whiteImage.width, u.whiteImage.height))

	if err := u.init(); err != nil {
		return nil, err
	}

	return u, nil
}

func (u *UserInterface) readPixels(mipmap *mipmap.Mipmap, pixels []byte, region image.Rectangle) error {
	return mipmap.ReadPixels(u.graphicsDriver, pixels, region)
}

func (u *UserInterface) dumpScreenshot(mipmap *mipmap.Mipmap, name string, blackbg bool) (string, error) {
	return mipmap.DumpScreenshot(u.graphicsDriver, name, blackbg)
}

func (u *UserInterface) dumpImages(dir string) (string, error) {
	return atlas.DumpImages(u.graphicsDriver, dir)
}

type RunOptions struct {
	GraphicsLibrary   GraphicsLibrary
	InitUnfocused     bool
	ScreenTransparent bool
	SkipTaskbar       bool
	SingleThread      bool
}

// InitialWindowPosition returns the position for centering the given second width/height pair within the first width/height pair.
func InitialWindowPosition(mw, mh, ww, wh int) (x, y int) {
	return (mw - ww) / 2, (mh - wh) / 3
}

func (u *UserInterface) error() error {
	u.errM.Lock()
	defer u.errM.Unlock()
	return u.err
}

func (u *UserInterface) setError(err error) {
	u.errM.Lock()
	defer u.errM.Unlock()
	if u.err == nil {
		u.err = err
	}
}

func (u *UserInterface) IsScreenClearedEveryFrame() bool {
	return atomic.LoadInt32(&u.isScreenClearedEveryFrame) != 0
}

func (u *UserInterface) SetScreenClearedEveryFrame(cleared bool) {
	v := int32(0)
	if cleared {
		v = 1
	}
	atomic.StoreInt32(&u.isScreenClearedEveryFrame, v)
}

func (u *UserInterface) setGraphicsLibrary(library GraphicsLibrary) {
	atomic.StoreInt32(&u.graphicsLibrary, int32(library))
}

func (u *UserInterface) GraphicsLibrary() GraphicsLibrary {
	return GraphicsLibrary(atomic.LoadInt32(&u.graphicsLibrary))
}

func (u *UserInterface) isRunning() bool {
	return atomic.LoadInt32(&u.running) != 0 && !u.isTerminated()
}

func (u *UserInterface) setRunning(running bool) {
	if running {
		atomic.StoreInt32(&u.running, 1)
	} else {
		atomic.StoreInt32(&u.running, 0)
	}
}

func (u *UserInterface) isTerminated() bool {
	return atomic.LoadInt32(&u.terminated) != 0
}

func (u *UserInterface) setTerminated() {
	atomic.StoreInt32(&u.terminated, 1)
}
