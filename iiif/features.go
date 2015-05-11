package iiif

// FeatureSet represents all possible IIIF 2.0 features that can be encoded
// into a URL.  The fields are the same as the string to report features,
// except that the first character should be lowercased.
//
// Note that using this in a custom server only gets you so far.  As noted in
// the Supported() documentation below, verifying complete support is trickier
// than just checking a URL, and a server that doesn't support arbitrary
// resizing can still advertise specific sizes that will work.
type FeatureSet struct {
	// Region options: note that full isn't specified but must be supported
	RegionByPx  bool
	RegionByPct bool

	// Size options: note that full isn't specified but must be supported
	SizeByWhListed bool
	SizeByW        bool
	SizeByH        bool
	SizeByPct      bool
	SizeByForcedWh bool
	SizeByWh       bool
	SizeAboveFull  bool

	// Rotation and mirroring
	RotationBy90s     bool
	RotationArbitrary bool
	Mirroring         bool

	// "Quality", or as normal folk call it, "color depth"
	Default bool
	Color   bool
	Gray    bool
	Bitonal bool

	// Format
	Jpg  bool
	Png  bool
	Tif  bool
	Gif  bool
	Jp2  bool
	Pdf  bool
	Webp bool

	// HTTP features
	BaseUriRedirect     bool
	Cors                bool
	JsonldMediaType     bool
	ProfileLinkHeader   bool
	CanonicalLinkHeader bool
}

// FeaturesLevel0: the required features for a level-0-compliant IIIF server
var FeaturesLevel0 = &FeatureSet{
	SizeByWhListed: true,
	Default:        true,
	Jpg:            true,
}

// FeaturesLevel1: the required features for a level-1-compliant IIIF server
var FeaturesLevel1 = &FeatureSet{
	RegionByPx:      true,
	SizeByWhListed:  true,
	SizeByW:         true,
	SizeByH:         true,
	SizeByPct:       true,
	Default:         true,
	Jpg:             true,
	BaseUriRedirect: true,
	Cors:            true,
	JsonldMediaType: true,
}

// FeaturesLevel2: the required features for a level-2-compliant IIIF server
var FeaturesLevel2 = &FeatureSet{
	RegionByPx:      true,
	RegionByPct:     true,
	SizeByWhListed:  true,
	SizeByW:         true,
	SizeByH:         true,
	SizeByPct:       true,
	SizeByForcedWh:  true,
	SizeByWh:        true,
	RotationBy90s:   true,
	Default:         true,
	Color:           true,
	Gray:            true,
	Bitonal:         true,
	Jpg:             true,
	Png:             true,
	BaseUriRedirect: true,
	Cors:            true,
	JsonldMediaType: true,
}

// Supported tells us whether or not the given feature set will actually
// perform the operation represented by the URL instance.
//
// Unsupported functionality is expected to report an http status of 501.
//
// This doesn't actually work in all cases, such as a level 0 server that has
// sizes explicitly listed for a given image resize operation.  In those cases,
// Supported() is probably not worth calling, instead handling just the few
// supported cases directly and/or checking a custom featureset directly.
//
// This also doesn't actually check all possibly supported features - the URL
// type is useful for parsing a URI path, but doesn't know about e.g.  http
// features.
func (fs *FeatureSet) Supported(u *URL) bool {
	return fs.SupportsRegion(u.Region) &&
		fs.SupportsSize(u.Size) &&
		fs.SupportsRotation(u.Rotation) &&
		fs.SupportsQuality(u.Quality) &&
		fs.SupportsFormat(u.Format)
}

// SupportsRegion just verifies a given region type is supported
func (fs *FeatureSet) SupportsRegion(r Region) bool {
	switch r.Type {
	case RTPixel:
		return fs.RegionByPx
	case RTPercent:
		return fs.RegionByPct
	default:
		return true
	}
}

// SupportsSize just verifies a given size type is supported
func (fs *FeatureSet) SupportsSize(s Size) bool {
	switch s.Type {
	case STScaleToWidth:
		return fs.SizeByW
	case STScaleToHeight:
		return fs.SizeByH
	case STScalePercent:
		return fs.SizeByPct
	case STExact:
		return fs.SizeByForcedWh
	case STBestFit:
		return fs.SizeByWh
	default:
		return true
	}
}

// SupportsRotation just verifies a given rotation type is supported
func (fs *FeatureSet) SupportsRotation(r Rotation) bool {
	// We check mirroring specially in order to make the degree checks simple
	if r.Mirror && !fs.Mirroring {
		return false
	}

	switch r.Degrees {
	case 0:
		return true
	case 90, 180, 270:
		return fs.RotationBy90s || fs.RotationArbitrary
	default:
		return fs.RotationArbitrary
	}
}

// SupportsQuality just verifies a given quality type is supported
func (fs *FeatureSet) SupportsQuality(q Quality) bool {
	switch q {
	case QColor:
		return fs.Color
	case QGray:
		return fs.Gray
	case QBitonal:
		return fs.Bitonal
	case QDefault, QNative:
		return fs.Default
	default:
		return false
	}
}

// SupportsFormat just verifies a given format type is supported
func (fs *FeatureSet) SupportsFormat(f Format) bool {
	switch f {
	case FmtJPG:
		return fs.Jpg
	case FmtTIF:
		return fs.Tif
	case FmtPNG:
		return fs.Png
	case FmtGIF:
		return fs.Gif
	case FmtJP2:
		return fs.Jp2
	case FmtPDF:
		return fs.Pdf
	case FmtWEBP:
		return fs.Webp
	default:
		return false
	}
}
