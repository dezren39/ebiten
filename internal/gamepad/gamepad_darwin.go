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

//go:build !ios
// +build !ios

package gamepad

import (
	"fmt"
	"sort"
	"unsafe"
)

// #cgo LDFLAGS: -framework CoreFoundation -framework IOKit
//
// #include <ForceFeedback/ForceFeedback.h>
// #include <IOKit/hid/IOHIDLib.h>
//
// static CFStringRef cfStringRefIOHIDVendorIDKey() {
//   return CFSTR(kIOHIDVendorIDKey);
// }
//
// static CFStringRef cfStringRefIOHIDProductIDKey() {
//   return CFSTR(kIOHIDProductIDKey);
// }
//
// static CFStringRef cfStringRefIOHIDVersionNumberKey() {
//   return CFSTR(kIOHIDVersionNumberKey);
// }
//
// static CFStringRef cfStringRefIOHIDProductKey() {
//   return CFSTR(kIOHIDProductKey);
// }
//
// static CFStringRef cfStringRefIOHIDDeviceUsagePageKey() {
//   return CFSTR(kIOHIDDeviceUsagePageKey);
// }
//
// static CFStringRef cfStringRefIOHIDDeviceUsageKey() {
//   return CFSTR(kIOHIDDeviceUsageKey);
// }
//
// void ebitenGamepadMatchingCallback(void *ctx, IOReturn res, void *sender, IOHIDDeviceRef device);
// void ebitenGamepadRemovalCallback(void *ctx, IOReturn res, void *sender, IOHIDDeviceRef device);
import "C"

type nativeGamepads struct {
	hidManager C.IOHIDManagerRef
}

type nativeGamepad struct {
	device  C.IOHIDDeviceRef
	axes    elements
	buttons elements
	hats    elements

	axisValues   []float64
	buttonValues []bool
	hatValues    []int
}

type element struct {
	native  C.IOHIDElementRef
	usage   int
	index   int
	minimum int
	maximum int
}

type elements []element

func (e elements) Len() int {
	return len(e)
}

func (e elements) Less(i, j int) bool {
	if e[i].usage != e[j].usage {
		return e[i].usage < e[j].usage
	}
	if e[i].index != e[j].index {
		return e[i].index < e[j].index
	}
	return false
}

func (e elements) Swap(i, j int) {
	e[i], e[j] = e[j], e[i]
}

func (g *nativeGamepad) present() bool {
	return g.device != 0
}

func (g *nativeGamepad) elementValue(e *element) int {
	if g.device == 0 {
		return 0
	}

	var valueRef C.IOHIDValueRef
	if C.IOHIDDeviceGetValue(g.device, e.native, &valueRef) == C.kIOReturnSuccess {
		return int(C.IOHIDValueGetIntegerValue(valueRef))
	}

	return 0
}

func (g *nativeGamepad) update() {
	if cap(g.axisValues) < len(g.axes) {
		g.axisValues = make([]float64, len(g.axes))
	}
	g.axisValues = g.axisValues[:len(g.axes)]

	if cap(g.buttonValues) < len(g.buttons) {
		g.buttonValues = make([]bool, len(g.buttons))
	}
	g.buttonValues = g.buttonValues[:len(g.buttons)]

	if cap(g.hatValues) < len(g.hats) {
		g.hatValues = make([]int, len(g.hats))
	}
	g.hatValues = g.hatValues[:len(g.hats)]

	for i, a := range g.axes {
		raw := g.elementValue(&a)
		if raw < a.minimum {
			a.minimum = raw
		}
		if raw > a.maximum {
			a.maximum = raw
		}
		var value float64
		if size := a.maximum - a.minimum; size != 0 {
			value = 2*float64(raw-a.minimum)/float64(size) - 1
		}
		g.axisValues[i] = value
	}

	for i, b := range g.buttons {
		g.buttonValues[i] = g.elementValue(&b) > 0
	}

	hatStates := []int{
		hatUp,
		hatRightUp,
		hatRight,
		hatRightDown,
		hatDown,
		hatLeftDown,
		hatLeft,
		hatLeftUp,
	}
	for i, h := range g.hats {
		if state := g.elementValue(&h); state < 0 || state >= len(hatStates) {
			g.hatValues[i] = hatCentered
		} else {
			g.hatValues[i] = hatStates[state]
		}
	}
}

func (g *nativeGamepad) axisNum() int {
	return len(g.axisValues)
}

func (g *nativeGamepad) buttonNum() int {
	return len(g.buttonValues)
}

func (g *nativeGamepad) hatNum() int {
	return len(g.hatValues)
}

func (g *nativeGamepad) axisValue(axis int) float64 {
	if axis < 0 || axis >= len(g.axisValues) {
		return 0
	}
	return g.axisValues[axis]
}

func (g *nativeGamepad) isButtonPressed(button int) bool {
	if button < 0 || button >= len(g.buttonValues) {
		return false
	}
	return g.buttonValues[button]
}

func (g *nativeGamepad) hatState(hat int) int {
	if hat < 0 || hat >= len(g.hatValues) {
		return hatCentered
	}
	return g.hatValues[hat]
}

func (g *nativeGamepads) init() {
	var dicts []unsafe.Pointer

	page := C.kHIDPage_GenericDesktop
	for _, usage := range []uint{
		C.kHIDUsage_GD_Joystick,
		C.kHIDUsage_GD_GamePad,
		C.kHIDUsage_GD_MultiAxisController,
	} {
		pageRef := C.CFNumberCreate(C.kCFAllocatorDefault, C.kCFNumberIntType, unsafe.Pointer(&page))
		if pageRef == 0 {
			panic("gamepad: CFNumberCreate returned nil")
		}
		defer C.CFRelease(C.CFTypeRef(pageRef))

		usageRef := C.CFNumberCreate(C.kCFAllocatorDefault, C.kCFNumberIntType, unsafe.Pointer(&usage))
		if usageRef == 0 {
			panic("gamepad: CFNumberCreate returned nil")
		}
		defer C.CFRelease(C.CFTypeRef(usageRef))

		keys := []unsafe.Pointer{
			unsafe.Pointer(C.cfStringRefIOHIDDeviceUsagePageKey()),
			unsafe.Pointer(C.cfStringRefIOHIDDeviceUsageKey()),
		}
		values := []unsafe.Pointer{
			unsafe.Pointer(pageRef),
			unsafe.Pointer(usageRef),
		}

		dict := C.CFDictionaryCreate(C.kCFAllocatorDefault, &keys[0], &values[0], C.CFIndex(len(keys)), &C.kCFTypeDictionaryKeyCallBacks, &C.kCFTypeDictionaryValueCallBacks)
		if dict == 0 {
			panic("gamepad: CFDictionaryCreate returned nil")
		}
		defer C.CFRelease(C.CFTypeRef(unsafe.Pointer(dict)))

		dicts = append(dicts, unsafe.Pointer(dict))
	}

	matching := C.CFArrayCreate(C.kCFAllocatorDefault, &dicts[0], C.CFIndex(len(dicts)), &C.kCFTypeArrayCallBacks)
	if matching == 0 {
		panic("gamepad: CFArrayCreateMutable returned nil")
	}
	defer C.CFRelease(C.CFTypeRef(matching))

	g.hidManager = C.IOHIDManagerCreate(C.kCFAllocatorDefault, C.kIOHIDOptionsTypeNone)
	if C.IOHIDManagerOpen(g.hidManager, C.kIOHIDOptionsTypeNone) != C.kIOReturnSuccess {
		panic("gamepad: IOHIDManagerOpen failed")
	}

	C.IOHIDManagerSetDeviceMatchingMultiple(g.hidManager, matching)
	C.IOHIDManagerRegisterDeviceMatchingCallback(g.hidManager, C.IOHIDDeviceCallback(C.ebitenGamepadMatchingCallback), nil)
	C.IOHIDManagerRegisterDeviceRemovalCallback(g.hidManager, C.IOHIDDeviceCallback(C.ebitenGamepadRemovalCallback), nil)

	C.IOHIDManagerScheduleWithRunLoop(g.hidManager, C.CFRunLoopGetMain(), C.kCFRunLoopDefaultMode)

	// Execute the run loop once in order to register any initially-attached gamepads.
	C.CFRunLoopRunInMode(C.kCFRunLoopDefaultMode, 0, 0 /* false */)
}

//export ebitenGamepadMatchingCallback
func ebitenGamepadMatchingCallback(ctx unsafe.Pointer, res C.IOReturn, sender unsafe.Pointer, device C.IOHIDDeviceRef) {
	if theGamepads.find(func(g *Gamepad) bool {
		return g.device == device
	}) != nil {
		return
	}

	name := "Unknown"
	if prop := C.IOHIDDeviceGetProperty(device, C.cfStringRefIOHIDProductKey()); prop != 0 {
		var cstr [256]C.char
		C.CFStringGetCString(C.CFStringRef(prop), &cstr[0], C.CFIndex(len(cstr)), C.kCFStringEncodingUTF8)
		name = C.GoString(&cstr[0])
	}

	var vendor uint32
	if prop := C.IOHIDDeviceGetProperty(device, C.cfStringRefIOHIDVendorIDKey()); prop != 0 {
		C.CFNumberGetValue(C.CFNumberRef(prop), C.kCFNumberSInt32Type, unsafe.Pointer(&vendor))
	}

	var product uint32
	if prop := C.IOHIDDeviceGetProperty(device, C.cfStringRefIOHIDProductIDKey()); prop != 0 {
		C.CFNumberGetValue(C.CFNumberRef(prop), C.kCFNumberSInt32Type, unsafe.Pointer(&product))
	}

	var version uint32
	if prop := C.IOHIDDeviceGetProperty(device, C.cfStringRefIOHIDVersionNumberKey()); prop != 0 {
		C.CFNumberGetValue(C.CFNumberRef(prop), C.kCFNumberSInt32Type, unsafe.Pointer(&version))
	}

	var sdlID string
	if vendor != 0 && product != 0 {
		sdlID = fmt.Sprintf("03000000%02x%02x0000%02x%02x0000%02x%02x0000",
			byte(vendor), byte(vendor>>8),
			byte(product), byte(product>>8),
			byte(version), byte(version>>8))
	} else {
		bs := []byte(name)
		if len(bs) < 12 {
			bs = append(bs, make([]byte, 12-len(bs))...)
		}
		sdlID = fmt.Sprintf("05000000%02x%02x%02x%02x%02x%02x%02x%02x%02x%02x%02x%02x",
			bs[0], bs[1], bs[2], bs[3], bs[4], bs[5], bs[6], bs[7], bs[8], bs[9], bs[10], bs[11])
	}

	elements := C.IOHIDDeviceCopyMatchingElements(device, 0, C.kIOHIDOptionsTypeNone)
	defer C.CFRelease(C.CFTypeRef(elements))

	g := theGamepads.add(name, sdlID)
	g.device = device

	for i := C.CFIndex(0); i < C.CFArrayGetCount(elements); i++ {
		native := (C.IOHIDElementRef)(C.CFArrayGetValueAtIndex(elements, i))
		if C.CFGetTypeID(C.CFTypeRef(native)) != C.IOHIDElementGetTypeID() {
			continue
		}

		typ := C.IOHIDElementGetType(native)
		if typ != C.kIOHIDElementTypeInput_Axis &&
			typ != C.kIOHIDElementTypeInput_Button &&
			typ != C.kIOHIDElementTypeInput_Misc {
			continue
		}

		usage := C.IOHIDElementGetUsage(native)
		page := C.IOHIDElementGetUsagePage(native)

		switch page {
		case C.kHIDPage_GenericDesktop:
			switch usage {
			case C.kHIDUsage_GD_X, C.kHIDUsage_GD_Y, C.kHIDUsage_GD_Z,
				C.kHIDUsage_GD_Rx, C.kHIDUsage_GD_Ry, C.kHIDUsage_GD_Rz,
				C.kHIDUsage_GD_Slider, C.kHIDUsage_GD_Dial, C.kHIDUsage_GD_Wheel:
				g.axes = append(g.axes, element{
					native:  native,
					usage:   int(usage),
					index:   len(g.axes),
					minimum: int(C.IOHIDElementGetLogicalMin(native)),
					maximum: int(C.IOHIDElementGetLogicalMax(native)),
				})
			case C.kHIDUsage_GD_Hatswitch:
				g.hats = append(g.hats, element{
					native:  native,
					usage:   int(usage),
					index:   len(g.hats),
					minimum: int(C.IOHIDElementGetLogicalMin(native)),
					maximum: int(C.IOHIDElementGetLogicalMax(native)),
				})
			case C.kHIDUsage_GD_DPadUp, C.kHIDUsage_GD_DPadRight, C.kHIDUsage_GD_DPadDown, C.kHIDUsage_GD_DPadLeft,
				C.kHIDUsage_GD_SystemMainMenu, C.kHIDUsage_GD_Select, C.kHIDUsage_GD_Start:
				g.buttons = append(g.buttons, element{
					native:  native,
					usage:   int(usage),
					index:   len(g.buttons),
					minimum: int(C.IOHIDElementGetLogicalMin(native)),
					maximum: int(C.IOHIDElementGetLogicalMax(native)),
				})
			}
		case C.kHIDPage_Simulation:
			switch usage {
			case C.kHIDUsage_Sim_Accelerator, C.kHIDUsage_Sim_Brake, C.kHIDUsage_Sim_Throttle, C.kHIDUsage_Sim_Rudder, C.kHIDUsage_Sim_Steering:
				g.axes = append(g.axes, element{
					native:  native,
					usage:   int(usage),
					index:   len(g.axes),
					minimum: int(C.IOHIDElementGetLogicalMin(native)),
					maximum: int(C.IOHIDElementGetLogicalMax(native)),
				})
			}
		case C.kHIDPage_Button, C.kHIDPage_Consumer:
			g.buttons = append(g.buttons, element{
				native:  native,
				usage:   int(usage),
				index:   len(g.buttons),
				minimum: int(C.IOHIDElementGetLogicalMin(native)),
				maximum: int(C.IOHIDElementGetLogicalMax(native)),
			})
		}
	}

	sort.Stable(g.axes)
	sort.Stable(g.buttons)
	sort.Stable(g.hats)
}

//export ebitenGamepadRemovalCallback
func ebitenGamepadRemovalCallback(ctx unsafe.Pointer, res C.IOReturn, sender unsafe.Pointer, device C.IOHIDDeviceRef) {
	theGamepads.remove(func(g *Gamepad) bool {
		return g.device == device
	})
}
