package media

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// Validation tolerances. Deliberately loose — the goal is to catch outputs that
// are broken, not to second-guess the encoder on legitimate variation.
const (
	// durationTolerancePct is the allowed drift between source and output
	// duration, as a fraction. Container overhead and frame-rate rounding
	// account for well under 1% on a real transcode.
	durationTolerancePct = 0.02

	// durationToleranceFloorSec is the minimum absolute drift allowed, so short
	// clips aren't failed for a rounding difference.
	durationToleranceFloorSec = 5.0

	// minSizeRatio is the smallest plausible output size as a fraction of the
	// source. A REMUX can legitimately compress to a few percent, so this only
	// needs to catch stubs and truncation.
	minSizeRatio = 0.005

	// minAbsoluteSizeBytes rejects trivially small outputs regardless of source
	// size — nothing playable comes out of this pipeline under 1 MB.
	minAbsoluteSizeBytes = 1 << 20
)

// OutputValidationError describes why a transcode output was rejected.
type OutputValidationError struct {
	Path    string
	Reasons []string
}

func (e *OutputValidationError) Error() string {
	return fmt.Sprintf("output validation failed for %s: %s",
		e.Path, strings.Join(e.Reasons, "; "))
}

// ValidateOutput checks that a freshly written output file is a plausible
// transcode of its source, and returns an *OutputValidationError describing
// every problem found if it is not.
//
// This is the deterministic gate that must pass before a job is reported as
// successful or a source file is considered safe to delete. A non-zero exit
// from FFmpeg is not the only failure mode: a transcode can also stop early and
// leave a well-formed but truncated file, which an existence-and-size check
// happily accepts.
func (f *FFmpegWrapper) ValidateOutput(ctx context.Context, src *MediaInfo, outputPath string) error {
	var reasons []string

	info, err := os.Stat(outputPath)
	if err != nil {
		return &OutputValidationError{
			Path:    outputPath,
			Reasons: []string{fmt.Sprintf("output file is missing (%v)", err)},
		}
	}
	if info.IsDir() {
		return &OutputValidationError{
			Path:    outputPath,
			Reasons: []string{"output path is a directory, not a file"},
		}
	}

	// Size checks. Cheap, and they catch the 0-byte and stub cases outright.
	outSize := info.Size()
	if outSize < minAbsoluteSizeBytes {
		reasons = append(reasons, fmt.Sprintf("output is %d bytes, below the %d byte floor",
			outSize, minAbsoluteSizeBytes))
	}
	if src != nil && src.Size > 0 {
		if ratio := float64(outSize) / float64(src.Size); ratio < minSizeRatio {
			reasons = append(reasons, fmt.Sprintf(
				"output is %.4f%% of the source (%d vs %d bytes), below the %.2f%% floor",
				ratio*100, outSize, src.Size, minSizeRatio*100))
		}
	}

	// If the file is already obviously broken, don't spend an ffprobe on it.
	if len(reasons) > 0 {
		return &OutputValidationError{Path: outputPath, Reasons: reasons}
	}

	out, err := f.GetMediaInfo(ctx, outputPath)
	if err != nil {
		return &OutputValidationError{
			Path:    outputPath,
			Reasons: []string{fmt.Sprintf("output is not probeable, likely corrupt (%v)", err)},
		}
	}

	if out.VideoStreams == 0 {
		reasons = append(reasons, "output contains no video stream")
	}

	if src != nil {
		// Duration is the strongest signal available: a transcode that died
		// partway through produces a file whose duration is short, however
		// well-formed the container looks.
		if src.Duration > 0 {
			if out.Duration <= 0 {
				reasons = append(reasons, "output has no readable duration")
			} else {
				tolerance := src.Duration * durationTolerancePct
				if tolerance < durationToleranceFloorSec {
					tolerance = durationToleranceFloorSec
				}
				if drift := src.Duration - out.Duration; drift > tolerance {
					reasons = append(reasons, fmt.Sprintf(
						"output is %.1fs shorter than the source (%.1fs vs %.1fs, tolerance %.1fs) — truncated",
						drift, out.Duration, src.Duration, tolerance))
				}
			}
		}

		// Losing every audio track means the map or the encode went wrong.
		if src.AudioStreams > 0 && out.AudioStreams == 0 {
			reasons = append(reasons, fmt.Sprintf(
				"source had %d audio stream(s), output has none", src.AudioStreams))
		}
	}

	if len(reasons) > 0 {
		return &OutputValidationError{Path: outputPath, Reasons: reasons}
	}
	return nil
}

// ValidateExtractedOutput checks that a freshly extracted MKV is a complete,
// probeable copy of the disc title it came from.
//
// Disc extraction has no encode step to fail mid-stream the way a transcode
// does, but MakeMKV can still be interrupted — a scratched disc, a drive that
// drops mid-read — and leave a truncated file behind while exiting 0. A bare
// existence-and-nonzero-size check accepts that as a complete extraction,
// which is how a source disc image has been deleted while the only remaining
// copy was a stub.
//
// expectedDurationSec is the duration MakeMKV itself reported for the title
// during the scan (DiscInfo.TitleDurationSeconds) — ffprobe cannot read the
// disc directly, so this is the only source duration available. Pass 0 to
// skip the duration check when it isn't known.
func (f *FFmpegWrapper) ValidateExtractedOutput(ctx context.Context, expectedDurationSec float64, outputPath string) error {
	var reasons []string

	info, err := os.Stat(outputPath)
	if err != nil {
		return &OutputValidationError{
			Path:    outputPath,
			Reasons: []string{fmt.Sprintf("output file is missing (%v)", err)},
		}
	}
	if info.IsDir() {
		return &OutputValidationError{
			Path:    outputPath,
			Reasons: []string{"output path is a directory, not a file"},
		}
	}
	if outSize := info.Size(); outSize < minAbsoluteSizeBytes {
		return &OutputValidationError{
			Path: outputPath,
			Reasons: []string{fmt.Sprintf("output is %d bytes, below the %d byte floor",
				outSize, minAbsoluteSizeBytes)},
		}
	}

	out, err := f.GetMediaInfo(ctx, outputPath)
	if err != nil {
		return &OutputValidationError{
			Path:    outputPath,
			Reasons: []string{fmt.Sprintf("output is not probeable, likely corrupt (%v)", err)},
		}
	}

	if out.VideoStreams == 0 {
		reasons = append(reasons, "output contains no video stream")
	}

	if expectedDurationSec > 0 {
		if out.Duration <= 0 {
			reasons = append(reasons, "output has no readable duration")
		} else {
			tolerance := expectedDurationSec * durationTolerancePct
			if tolerance < durationToleranceFloorSec {
				tolerance = durationToleranceFloorSec
			}
			if drift := expectedDurationSec - out.Duration; drift > tolerance {
				reasons = append(reasons, fmt.Sprintf(
					"output is %.1fs shorter than the disc title reported (%.1fs vs %.1fs, tolerance %.1fs) — truncated",
					drift, out.Duration, expectedDurationSec, tolerance))
			}
		}
	}

	if len(reasons) > 0 {
		return &OutputValidationError{Path: outputPath, Reasons: reasons}
	}
	return nil
}

// MeetsSavingsFloor reports whether outSize is at least floor smaller than
// srcSize, as a fraction (0.15 == 15%).
//
// ValidateOutput only catches outputs that are broken; it says nothing about
// outputs that are simply not worth having. A transcode that comes out the
// same size as its source, or bigger, is not an optimisation — replacing a
// good file with a same-size or larger one is a regression a job should never
// report as a success.
//
// srcSize <= 0 reports true: there is nothing to compare against, and that
// case is for ValidateOutput's size checks to catch, not this one.
func MeetsSavingsFloor(srcSize, outSize int64, floor float64) bool {
	if srcSize <= 0 {
		return true
	}
	savings := 1 - float64(outSize)/float64(srcSize)
	return savings >= floor
}

// DefaultDensityFloor is the bits-per-pixel-per-frame density at or below
// which an HEVC or AV1 source is treated as already an efficient encode.
//
// This is a rough heuristic, not a quality measurement — bpp varies with
// content complexity and resolution, and the same value doesn't mean the same
// thing for a 1080p WEB-DL and a 2160p REMUX. It is deliberately set low
// enough that only sources clearly at or below what this pipeline's own HEVC
// output would land at get skipped; anything borderline is left to go through
// the encoder rather than risk skipping a source that would actually benefit.
// A sample-encode-and-measure-VMAF approach would be more accurate but costs
// a partial encode per candidate; this is the zero-latency first tier.
const DefaultDensityFloor = 0.06

// SkipEncodeError signals that a source should not be re-encoded — not
// because the pipeline cannot handle it (CheckSourceSupported's plain errors
// are for that), but because doing so would not improve it. Callers should
// complete the job as a successful no-op rather than treating this as a
// failure.
type SkipEncodeError struct {
	Reason string
}

func (e *SkipEncodeError) Error() string { return e.Reason }

// IsAlreadyEfficient reports whether a source encoded in codec, with the
// given bits-per-pixel-per-frame density, is efficient enough that
// re-encoding it again is not worth the generational quality loss.
//
// Keyed on codec identity as well as density: an H.264 source at the same
// density as a good HEVC encode still benefits from moving to a more
// efficient codec, so this only ever applies to sources already in HEVC or
// AV1 — the codecs this pipeline itself would produce.
func IsAlreadyEfficient(codec string, bitsPerPixel, floor float64) bool {
	switch strings.ToLower(codec) {
	case "hevc", "h265", "av1":
		return bitsPerPixel > 0 && bitsPerPixel <= floor
	default:
		return false
	}
}

// CheckSourceSupported reports whether a source can be transcoded correctly
// with the current pipeline, returning a descriptive error when it cannot —
// or a *SkipEncodeError when it can, but re-encoding it would not help.
//
// Dolby Vision profile 5 has no HDR10 base layer. Re-encoding it discards the
// RPU metadata that carries the colour transform, so the result plays back with
// badly shifted colour — green casts and crushed highlights. Profiles 7 and 8
// carry a conventional HDR10 base layer that survives the re-encode; the Dolby
// Vision layer is lost, but the output is correct HDR10.
//
// densityFloor is compared against the source's BitsPerPixel() — see
// IsAlreadyEfficient and DefaultDensityFloor.
func CheckSourceSupported(src *MediaInfo, densityFloor float64) error {
	if src == nil {
		return nil
	}
	if src.DVProfile == 5 {
		return fmt.Errorf(
			"unsupported input: Dolby Vision profile 5 has no HDR10 base layer and "+
				"cannot be re-encoded without tonemapping (%s)", src.Filename)
	}
	if bpp := src.BitsPerPixel(); IsAlreadyEfficient(src.CodecName, bpp, densityFloor) {
		return &SkipEncodeError{Reason: fmt.Sprintf(
			"source is already %s at %.3f bits/pixel (floor %.3f) — re-encoding would be "+
				"generational loss for no size benefit (%s)",
			strings.ToUpper(src.CodecName), bpp, densityFloor, src.Filename)}
	}
	return nil
}
