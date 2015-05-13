package openjpeg

// #cgo LDFLAGS: -lopenjp2
// #include <openjpeg-2.1/openjpeg.h>
import "C"

import (
	"errors"
	"fmt"
	"image"
	"reflect"
	"runtime"
	"unsafe"

	"github.com/nfnt/resize"
)

// Container for our simple JP2 operations
type JP2Image struct {
	filename        string
	stream          *C.opj_stream_t
	codec           *C.opj_codec_t
	image           *C.opj_image_t
	decodeWidth     int
	decodeHeight    int
	scaleFactor     float64
	decodeArea      image.Rectangle
	crop            bool
	resizeByPercent bool
	resizeByPixels  bool
}

func NewJP2Image(filename string) (*JP2Image, error) {
	i := &JP2Image{filename: filename}
	runtime.SetFinalizer(i, finalizer)

	if err := i.initializeStream(); err != nil {
		return nil, err
	}

	return i, nil
}

// SetScale sets the image to scale by the given multiplier, typically a
// percentage from 0 to 1.  This is mutually exclusive with resizing by a set
// width/height value.
func (i *JP2Image) SetScale(m float64) {
	i.scaleFactor = m
	i.resizeByPercent = true
	i.resizeByPixels = false
}

// SetResizeWH sets the image to scale to the given width and height.  If one
// dimension is 0, the decoded image will preserve the aspect ratio while
// scaling to the non-zero dimension.
func (i *JP2Image) SetResizeWH(width, height int) {
	i.decodeWidth = width
	i.decodeHeight = height
	i.resizeByPixels = true
	i.resizeByPercent = false
}

func (i *JP2Image) SetCrop(r image.Rectangle) {
	i.decodeArea = r
	i.crop = true
}

// DecodeImage returns an image.Image that holds the decoded image data,
// resized and cropped if resizing or cropping was requested.  Both cropping
// and resizing happen here due to the nature of openjpeg, so SetScale,
// SetResizeWH, and SetCrop must be called before this function.
func (i *JP2Image) DecodeImage() (image.Image, error) {
	// We need the codec to be ready for all operations below
	if err := i.initializeCodec(); err != nil {
		goLog(3, "Error initializing codec - aborting")
		return nil, err
	}

	// If we want to resize, but not crop, we have to set the decode area to the
	// full image - which means reading in the image header and then cleaning up
	// all previously-initialized data
	if (i.resizeByPixels || i.resizeByPercent) && !i.crop {
		var err error
		i.decodeArea, err = i.GetDimensions()
		if err != nil {
			goLog(3, "Error getting dimensions - aborting")
			return nil, err
		}
	}

	// If resize is by percent, we now have the decode area, and can use that to
	// get pixel dimensions
	if i.resizeByPercent {
		i.decodeWidth = int(float64(i.decodeArea.Dx()) * i.scaleFactor)
		i.decodeHeight = int(float64(i.decodeArea.Dy()) * i.scaleFactor)
		i.resizeByPixels = true
	}

	// Get progression level if we're resizing to specific dimensions (it's zero
	// if there isn't any scaling of the output)
	if i.resizeByPixels {
		level := desiredProgressionLevel(i.decodeArea, i.decodeWidth, i.decodeHeight)
		if err := i.SetDynamicProgressionLevel(level); err != nil {
			goLog(3, "Unable to set dynamic progression level - aborting")
			return nil, err
		}
	}

	if err := i.ReadHeader(); err != nil {
		goLog(3, "Error reading header before decode - aborting")
		return nil, err
	}

	goLog(6, fmt.Sprintf("num comps: %d", i.image.numcomps))
	goLog(6, fmt.Sprintf("x0: %d, x1: %d, y0: %d, y1: %d", i.image.x0, i.image.x1, i.image.y0, i.image.y1))

	// Setting decode area has to happen *after* reading the header / image data
	if i.crop {
		r := i.decodeArea
		if C.opj_set_decode_area(i.codec, i.image, C.OPJ_INT32(r.Min.X), C.OPJ_INT32(r.Min.Y), C.OPJ_INT32(r.Max.X), C.OPJ_INT32(r.Max.Y)) == C.OPJ_FALSE {
			return nil, errors.New("failed to set the decoded area")
		}
	}

	// Decode the JP2 into the image stream
	if C.opj_decode(i.codec, i.stream, i.image) == C.OPJ_FALSE {
		return nil, errors.New("failed to decode image")
	}
	if C.opj_end_decompress(i.codec, i.stream) == C.OPJ_FALSE {
		return nil, errors.New("failed to close decompression")
	}

	var comps []C.opj_image_comp_t
	compsSlice := (*reflect.SliceHeader)((unsafe.Pointer(&comps)))
	compsSlice.Cap = int(i.image.numcomps)
	compsSlice.Len = int(i.image.numcomps)
	compsSlice.Data = uintptr(unsafe.Pointer(i.image.comps))

	width := int(comps[0].w)
	height := int(comps[0].h)
	bounds := image.Rect(0, 0, width, height)
	var img image.Image

	// We assume grayscale if we don't have at least 3 components, because it's
	// probably the safest default
	if len(comps) < 3 {
		img = &image.Gray{Pix: JP2ComponentData(comps[0]), Stride: width, Rect: bounds}
	} else {
		// If we have 3+ components, we only care about the first three - I have no
		// idea what else we might have other than alpha, and as a tile server, we
		// don't care about the *source* image's alpha.  It's worth noting that
		// this will almost certainly blow up on any JP2 that isn't using RGB.

		area := width * height
		bytes := area << 2
		realData := make([]uint8, bytes)

		red := JP2ComponentData(comps[0])
		green := JP2ComponentData(comps[1])
		blue := JP2ComponentData(comps[2])

		offset := 0
		for i := 0; i < area; i++ {
			realData[offset] = red[i]
			offset++
			realData[offset] = green[i]
			offset++
			realData[offset] = blue[i]
			offset++
			realData[offset] = 255
			offset++
		}

		img = &image.RGBA{Pix: realData, Stride: width << 2, Rect: bounds}
	}

	if i.resizeByPixels {
		img = resize.Resize(uint(i.decodeWidth), uint(i.decodeHeight), img, resize.Bilinear)
	}

	return img, nil
}

func (i *JP2Image) ReadHeader() error {
	if i.image != nil {
		return nil
	}

	if err := i.initializeCodec(); err != nil {
		return err
	}

	if err := i.initializeStream(); err != nil {
		return err
	}

	if C.opj_read_header(i.stream, i.codec, &i.image) == C.OPJ_FALSE {
		return errors.New("failed to read the header")
	}

	return nil
}

// GetDimensions reads the JP2 headers and pulls dimensions in order to satisfy
// the IIIFImage interface.  The image resource is cleaned up afterward, as this
// operation has to be usable independently of decoding.
func (i *JP2Image) GetDimensions() (image.Rectangle, error) {
	if err := i.ReadHeader(); err != nil {
		return image.Rectangle{}, err
	}

	d := i.Dimensions()
	i.CleanupResources()
	return d, nil
}

func (i *JP2Image) Dimensions() image.Rectangle {
	return image.Rect(int(i.image.x0), int(i.image.y0), int(i.image.x1), int(i.image.y1))
}

// Attempts to set the progression level to the given value, then re-read the
// header.  If reading the header fails, attempts to set the level to one level
// below the initial level.  If reading the header fails at level 0, an error
// is returned.
func (i *JP2Image) SetDynamicProgressionLevel(level int) error {
	onErr := func(err error) error {
		if level > 0 {
			goLog(6, fmt.Sprintf("Unable to set progression level to %d; trying again (%s)", level, err))
			i.CleanupResources()
			return i.SetDynamicProgressionLevel(level - 1)
		}

		return err
	}

	goLog(6, fmt.Sprintf("Setting progression level to %d", level))

	if err := i.initializeCodec(); err != nil {
		return onErr(err)
	}

	if C.opj_set_decoded_resolution_factor(i.codec, C.OPJ_UINT32(level)) == C.OPJ_FALSE {
		return onErr(errors.New("Error trying to set decoded resolution factor"))
	}

	if err := i.ReadHeader(); err != nil {
		return onErr(err)
	}

	return nil
}

// JP2ComponentData returns a slice of Image-usable uint8s from the JP2 raw
// data in the given component struct
func JP2ComponentData(comp C.struct_opj_image_comp) []uint8 {
	var data []int32
	dataSlice := (*reflect.SliceHeader)((unsafe.Pointer(&data)))
	size := int(comp.w) * int(comp.h)
	dataSlice.Cap = size
	dataSlice.Len = size
	dataSlice.Data = uintptr(unsafe.Pointer(comp.data))

	realData := make([]uint8, len(data))
	for index, point := range data {
		realData[index] = uint8(point)
	}

	return realData
}
