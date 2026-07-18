package capture

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
)

func DecodeMJPEG(data []byte) (image.Image, error) {
	frame, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode MJPEG frame: %w", err)
	}
	return frame, nil
}

func DecodeYUYV(data []byte, width, height int) (image.Image, error) {
	required := width * height * 2
	if width <= 0 || height <= 0 || width%2 != 0 || len(data) < required {
		return nil, fmt.Errorf("invalid YUYV frame: got %d bytes for %dx%d", len(data), width, height)
	}
	frame := image.NewRGBA(image.Rect(0, 0, width, height))
	for pixel, offset := 0, 0; pixel < width*height; pixel, offset = pixel+2, offset+4 {
		y0, u, y1, v := int(data[offset]), int(data[offset+1]), int(data[offset+2]), int(data[offset+3])
		writeYUVPixel(frame.Pix[pixel*4:], y0, u, v)
		writeYUVPixel(frame.Pix[(pixel+1)*4:], y1, u, v)
	}
	return frame, nil
}

func writeYUVPixel(destination []byte, y, u, v int) {
	c, d, e := y-16, u-128, v-128
	if c < 0 {
		c = 0
	}
	destination[0] = clipped((298*c + 409*e + 128) >> 8)
	destination[1] = clipped((298*c - 100*d - 208*e + 128) >> 8)
	destination[2] = clipped((298*c + 516*d + 128) >> 8)
	destination[3] = 255
}

func clipped(value int) byte {
	if value < 0 {
		return 0
	}
	if value > 255 {
		return 255
	}
	return byte(value)
}
