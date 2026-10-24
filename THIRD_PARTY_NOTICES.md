# Third-party notices

Livery contains code ported from, and protocol knowledge obtained from, the projects below.

## alienfx-tools / AlienFX-SDK (MIT)

The Alienware APIv4 (AW-ELC) and APIv5 (per-key keyboard) command layouts in `alienfx.go`, and the Alienware m18 R2 light map in `layout.go`, are ported from https://github.com/T-Troll/alienfx-tools.

```
MIT License

Copyright (c) 2020 Rik Lain

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

## alienware-m18-rgb (MIT)

The m18 power-state slot sequence (`SetPowerSlots` in `alienfx.go`) follows the protocol notes in https://github.com/digin1/alienware-m18-rgb.

```
MIT License

Copyright (c) 2026 Digin Dominic

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

## OpenRGB (protocol reference only)

`openrgb.go` is an independent implementation of the OpenRGB SDK network protocol, and `logitech.go` implements Logitech HID++ 2.0 RGB features; the OpenRGB project (https://openrgb.org, GPL-2.0) was consulted as documentation for both. No OpenRGB source code is included in Livery.

## Glorious GMBK 75% (interoperability)

The GMBK lighting report layout in `glorious.go` was determined by studying how the vendor's configuration software talks to the keyboard, for the purpose of interoperability. No vendor code is included.

## Go x/sys (BSD-3-Clause)

Livery depends on `golang.org/x/sys`, Copyright 2009 The Go Authors, under the BSD 3-Clause license (fetched by the Go toolchain, not vendored).
