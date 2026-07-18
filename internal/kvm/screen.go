package kvm

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"strconv"
	"time"
)

type ScreenRequest struct {
	View    string `json:"view,omitempty" jsonschema:"Image view: original, coordinates, or both; defaults to coordinates."`
	Format  string `json:"format,omitempty" jsonschema:"Image encoding: jpeg or png; defaults to jpeg."`
	Quality int    `json:"quality,omitempty" jsonschema:"JPEG quality from 60 through 95, default 85; omit this field for PNG."`
}

type ScreenImage struct {
	View     string `json:"view"`
	MIMEType string `json:"mime_type"`
	Data     []byte `json:"-"`
}

type ScreenResult struct {
	Width      int           `json:"width"`
	Height     int           `json:"height"`
	CapturedAt time.Time     `json:"captured_at"`
	Signal     string        `json:"signal"`
	Images     []ScreenImage `json:"images"`
}

func (controller *MemoryController) GetScreen(_ context.Context, request ScreenRequest) (ScreenResult, error) {
	if !controller.status.Screen.Configured {
		return ScreenResult{}, capabilityNotConfigured("screen")
	}
	controller.mu.Lock()
	frame := controller.screen
	controller.mu.Unlock()
	if frame == nil {
		return ScreenResult{}, &OperationError{Code: ErrorNoVideoSignal, Message: "the screen source has no current frame", Recovery: "check the HDMI source and capture device"}
	}
	return renderScreen(frame, request)
}

func renderScreen(frame image.Image, request ScreenRequest) (ScreenResult, error) {
	if request.View == "" {
		request.View = "coordinates"
	}
	if request.Format == "" {
		request.Format = "jpeg"
	}
	if request.Quality == 0 && request.Format == "jpeg" {
		request.Quality = 85
	}
	if request.View != "original" && request.View != "coordinates" && request.View != "both" {
		return ScreenResult{}, invalidArgument("view must be original, coordinates, or both")
	}
	if request.Format != "jpeg" && request.Format != "png" {
		return ScreenResult{}, invalidArgument("format must be jpeg or png")
	}
	if request.Format == "png" && request.Quality != 0 {
		return ScreenResult{}, invalidArgument("quality cannot be provided for PNG")
	}
	if request.Format == "jpeg" && (request.Quality < 60 || request.Quality > 95) {
		return ScreenResult{}, invalidArgument("JPEG quality must be between 60 and 95")
	}

	views := []string{request.View}
	if request.View == "both" {
		views = []string{"original", "coordinates"}
	}
	result := ScreenResult{
		Width:      frame.Bounds().Dx(),
		Height:     frame.Bounds().Dy(),
		CapturedAt: time.Now().UTC(),
		Signal:     "present",
		Images:     make([]ScreenImage, 0, len(views)),
	}
	for _, view := range views {
		candidate := cloneImage(frame)
		if view == "coordinates" {
			annotateCoordinates(candidate)
		}
		data, mimeType, err := encodeScreen(candidate, request.Format, request.Quality)
		if err != nil {
			return ScreenResult{}, &OperationError{Code: ErrorInternal, Message: "could not encode screen image", Recovery: "inspect kvmctl diagnostics and retry"}
		}
		result.Images = append(result.Images, ScreenImage{View: view, MIMEType: mimeType, Data: data})
	}
	return result, nil
}

func cloneImage(source image.Image) *image.RGBA {
	bounds := source.Bounds()
	destination := image.NewRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	draw.Draw(destination, destination.Bounds(), source, bounds.Min, draw.Src)
	return destination
}

func encodeScreen(source image.Image, format string, quality int) ([]byte, string, error) {
	var buffer bytes.Buffer
	if format == "png" {
		if err := png.Encode(&buffer, source); err != nil {
			return nil, "", err
		}
		return buffer.Bytes(), "image/png", nil
	}
	if err := jpeg.Encode(&buffer, source, &jpeg.Options{Quality: quality}); err != nil {
		return nil, "", err
	}
	return buffer.Bytes(), "image/jpeg", nil
}

func annotateCoordinates(target *image.RGBA) {
	bounds := target.Bounds()
	line := color.RGBA{R: 0, G: 220, B: 255, A: 255}
	for value := 0; value <= 1000; value += 100 {
		x := value * (bounds.Dx() - 1) / 1000
		y := value * (bounds.Dy() - 1) / 1000
		for py := 0; py < bounds.Dy(); py++ {
			target.SetRGBA(x, py, line)
			if x+1 < bounds.Dx() {
				target.SetRGBA(x+1, py, line)
			}
		}
		for px := 0; px < bounds.Dx(); px++ {
			target.SetRGBA(px, y, line)
			if y+1 < bounds.Dy() {
				target.SetRGBA(px, y+1, line)
			}
		}
		drawNumber(target, x+2, 2, strconv.Itoa(value))
		drawNumber(target, 2, y+2, strconv.Itoa(value))
	}
}

var digitRows = map[byte][5]string{
	'0': {"111", "101", "101", "101", "111"},
	'1': {"010", "110", "010", "010", "111"},
	'2': {"111", "001", "111", "100", "111"},
	'3': {"111", "001", "111", "001", "111"},
	'4': {"101", "101", "111", "001", "001"},
	'5': {"111", "100", "111", "001", "111"},
	'6': {"111", "100", "111", "101", "111"},
	'7': {"111", "001", "001", "001", "001"},
	'8': {"111", "101", "111", "101", "111"},
	'9': {"111", "101", "111", "001", "111"},
}

func drawNumber(target *image.RGBA, x, y int, text string) {
	white := color.RGBA{R: 255, G: 255, B: 255, A: 255}
	for index := 0; index < len(text); index++ {
		rows := digitRows[text[index]]
		for row, pixels := range rows {
			for column := 0; column < len(pixels); column++ {
				if pixels[column] == '1' && image.Pt(x+index*4+column, y+row).In(target.Bounds()) {
					target.SetRGBA(x+index*4+column, y+row, white)
				}
			}
		}
	}
}
