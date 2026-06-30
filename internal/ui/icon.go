package ui

// trayIconBytes returns a 16x16 blue circle ICO.
// BMP data is bottom-up, AND mask = 1 for opaque pixels.
func trayIconBytes() []byte {
	const w, h = 16, 16
	le16 := func(b []byte, v uint16) { b[0], b[1] = byte(v), byte(v>>8) }
	le32 := func(b []byte, v uint32) { b[0], b[1], b[2], b[3] = byte(v), byte(v>>8), byte(v>>16), byte(v>>24) }

	// XOR mask: bottom-up rows
	xor := make([]byte, w*h*4)
	andRows := make([][2]byte, h)
	// Init AND mask to 0xFF = all transparent
	for i := range andRows {
		andRows[i] = [2]byte{0xFF, 0xFF}
	}

	for y := 0; y < h; y++ {
		by := h - 1 - y // ICO BMP is bottom-up
		for x := 0; x < w; x++ {
			o := (by*w + x) * 4
			dx, dy := float64(x-8), float64(y-8)
			if dx*dx+dy*dy <= 49 {
				xor[o], xor[o+1], xor[o+2], xor[o+3] = 0x78, 0x4f, 0x26, 0xFF
				// AND=0 → opaque (show XOR color)
				andRows[y][x/8] &^= 1 << (7 - uint(x%8))
			}
		}
	}

	and := make([]byte, 32)
	for i, row := range andRows {
		and[2*i], and[2*i+1] = row[0], row[1]
	}

	// BITMAPINFOHEADER (40 bytes)
	bh := make([]byte, 40)
	le32(bh, 40)
	le32(bh[4:], uint32(w))
	le32(bh[8:], uint32(h*2)) // double height for AND mask
	bh[12], bh[13] = 1, 0      // biPlanes=1
	bh[14], bh[15] = 32, 0     // biBitCount=32

	bmp := make([]byte, 0, 40+len(xor)+len(and))
	bmp = append(bmp, bh...)
	bmp = append(bmp, xor...)
	bmp = append(bmp, and...)

	// Build ICO: 6-byte header + 16-byte directory entry + BMP
	ico := make([]byte, 22+len(bmp))
	le16(ico[0:], 0)
	le16(ico[2:], 1) // ICO type
	le16(ico[4:], 1) // count
	ico[6], ico[7] = byte(w), byte(h)
	le16(ico[10:], 1)
	le16(ico[12:], 32)
	le32(ico[14:], uint32(len(bmp)))
	le32(ico[18:], 22)
	copy(ico[22:], bmp)
	return ico
}

// trayIconBytesYellow returns a 16x16 yellow circle ICO (warning indicator).
func trayIconBytesYellow() []byte { return trayIconBytesColor(0xF0, 0xB4, 0x00) }

func trayIconBytesColor(r, g, b byte) []byte {
	const w, h = 16, 16
	le16 := func(b []byte, v uint16) { b[0], b[1] = byte(v), byte(v>>8) }
	le32 := func(b []byte, v uint32) { b[0], b[1], b[2], b[3] = byte(v), byte(v>>8), byte(v>>16), byte(v>>24) }

	xor := make([]byte, w*h*4)
	andRows := make([][2]byte, h)
	for i := range andRows {
		andRows[i] = [2]byte{0xFF, 0xFF}
	}

	for y := 0; y < h; y++ {
		by := h - 1 - y
		for x := 0; x < w; x++ {
			o := (by*w + x) * 4
			dx, dy := float64(x-8), float64(y-8)
			if dx*dx+dy*dy <= 49 {
				xor[o], xor[o+1], xor[o+2], xor[o+3] = b, g, r, 0xFF
				andRows[y][x/8] &^= 1 << (7 - uint(x%8))
			}
		}
	}

	and := make([]byte, 32)
	for i, row := range andRows {
		and[2*i], and[2*i+1] = row[0], row[1]
	}

	bh := make([]byte, 40)
	le32(bh, 40)
	le32(bh[4:], uint32(w))
	le32(bh[8:], uint32(h*2))
	bh[12], bh[13] = 1, 0
	bh[14], bh[15] = 32, 0

	bmp := make([]byte, 0, 40+len(xor)+len(and))
	bmp = append(bmp, bh...)
	bmp = append(bmp, xor...)
	bmp = append(bmp, and...)

	ico := make([]byte, 22+len(bmp))
	le16(ico[0:], 0)
	le16(ico[2:], 1)
	le16(ico[4:], 1)
	ico[6], ico[7] = byte(w), byte(h)
	le16(ico[10:], 1)
	le16(ico[12:], 32)
	le32(ico[14:], uint32(len(bmp)))
	le32(ico[18:], 22)
	copy(ico[22:], bmp)
	return ico
}
