# Compressed music, M18

## Scope and ownership

This contract defines how Goje decodes a FLAC music track into the clip format that the mixer already plays, and how the Beacon music asset is made. The [M18 RFC](../rfcs/0019-m18-live-matches-and-distribution.md#compressed-music-and-beacon) records the decision to start with mono 48 kHz 16-bit FLAC and to decode before playback.

Goje owns [`audio.DecodeFLAC`](../audio/flac.go), `audio.MaxFLACBytes`, the Beacon music source in [`audio/testdata`](../audio/testdata), and the asset [`examples/beacon/assets/music.flac`](../examples/beacon/assets/music.flac). The decoded clip is the format in the [audio mixer specification](audio-mixer-m15.md) and the [music specification](music-m17.md): packed little-endian signed 16-bit mono samples at 48 kHz, at most 64 seconds (`audio.MaxMusicSeconds`). The mixer, its allocation contract, the music requests, and the score format do not change. Streaming, resampling, stereo, other bit depths, and other codecs are out of scope.

## Decoder

`audio.DecodeFLAC(r io.Reader) (audio.Clip, error)` reads one native FLAC stream ([RFC 9639](https://www.rfc-editor.org/rfc/rfc9639.html)) and returns its samples as a clip. It does not close `r`. On error, the clip is zero, so a caller that keeps its current track on error never plays a partial one. Decode during loading, not in the audio device callback.

The frame and subframe decoding uses [`github.com/mewkiz/flac`](https://github.com/mewkiz/flac) v1.0.14, package `frame` only. Goje reads the stream marker and metadata itself, so the dependency's metadata parsers, ID3 handling, and seeking never see untrusted input.

| Part | Rule |
| --- | --- |
| Size | The stream, metadata included, is at most 4 MiB (`audio.MaxFLACBytes`). A longer input is rejected, even when its first 4 MiB form a valid stream. |
| Marker | The input starts with `fLaC`. An ID3 tag or any other prefix is rejected. |
| STREAMINFO | The first metadata block is a 34-byte STREAMINFO with a sample rate of 48,000 Hz, one channel, 16 bits per sample, a nonzero total sample count of at most 64 × 48,000, a minimum block size of at least 16, and a maximum block size at least the minimum. |
| Other metadata | At most 16 further blocks of types 1 to 6 (padding, application, seek table, Vorbis comment, cue sheet, picture), with at most 64 KiB of contents in total. They are skipped without being parsed. A second STREAMINFO, a reserved type, or the invalid type 127 is rejected. |
| Frames | Each frame is mono, at 48 kHz and 16 bits per sample or deferring to STREAMINFO for both, and uses the blocking strategy of the first frame. Its frame or sample number is the next one in order. Its block size is from 1 to the STREAMINFO maximum, and at least the STREAMINFO minimum unless it is the last frame. |
| Samples | The frames decode to exactly the STREAMINFO sample count; a frame that would pass it is rejected before its subframe is decoded. Every decoded sample fits in 16 bits. Each frame's CRC-8 and CRC-16 must match. |
| End | No data follows the last frame. When the STREAMINFO MD5 signature is nonzero, it matches the decoded samples. |

A truncated stream is reported as truncated. Errors from the dependency are reported as text and not wrapped, so callers cannot depend on them.

### Limits on work and memory

The STREAMINFO checks bound the PCM buffer at 6,144,000 bytes before any frame is read, and the frame loop enforces the sample count again as frames arrive, so the metadata limit is not trusted alone. The buffer is allocated once at its final size. Each frame allocates one block of at most 65,535 32-bit samples, a slice of at most 32 LPC coefficients, and at most 32,768 residual partition descriptors; the block size is checked before those allocations. The compressed size limit bounds the bits the decoder can read, and so the work done for unary codes and escaped partitions. The stream is read through a 4 KiB buffer and is never held whole in memory.

The dependency logs one line through the standard `log` package when a frame header names a 176.4 kHz or 24 kHz sample rate. Goje rejects that frame before decoding it, so a malformed input logs at most one line per call.

## Beacon music

The source is the score [`audio/testdata/beacon-music.json`](../audio/testdata/beacon-music.json), a 16-second loop at 120 beats per minute over A minor, F, C, and G, in the canonical form of the [score format](music-m17.md#score-format-version-1). The source stays outside `examples/beacon/assets`, which Beacon embeds whole. Rebuild the asset from the repository root with:

```sh
go run audio/testdata/render-score.go < audio/testdata/beacon-music.json > /tmp/beacon-music.pcm
ffmpeg -hide_banner -y -f s16le -ar 48000 -ac 1 -i /tmp/beacon-music.pcm \
  -map_metadata -1 -fflags +bitexact -compression_level 8 examples/beacon/assets/music.flac
```

`render-score.go` writes the score's `audio.RenderScore` samples as raw PCM. The asset was made with FFmpeg 9.0.2. Another FFmpeg version may write different bytes, but the samples must not change: `TestDecodeFLACBeaconMusic` checks that the asset decodes to exactly the rendered score.

| Measure | Value |
| --- | --- |
| Duration | 768,000 frames, 16 seconds |
| Same samples as WAV | 1,536,044 bytes |
| FLAC | 686,299 bytes, 44.7% of the WAV |

## Measurements

These were measured with `BenchmarkDecodeFLACBeaconMusic` on an Apple M4 Pro with Go 1.27.4, three runs each. WebAssembly ran under Node.js 24 through `go_js_wasm_exec`.

| Host | Decode time | Allocated per decode | Allocations |
| --- | --- | --- | --- |
| Desktop, darwin/arm64 | 20.4 to 20.9 ms | 5,331,826 bytes | 2,888 |
| WebAssembly, js/wasm | 83.1 to 86.1 ms | 5,331,685 bytes | 2,888 |

The allocated bytes are the total for one decode, not the peak. They are the 1,536,000-byte clip, one 16 KiB block of samples for each of the 188 frames, and small per-frame state. Only the clip and the current frame are live at once.

For an upper-bound peak measurement, a fresh Go process loaded the input, forced a collection, disabled GC, and measured `runtime.MemStats.HeapAlloc` immediately before and after one decode. Keeping the result live, the maximum of three runs was 5,331,672 bytes on desktop and 5,331,624 bytes in Node WebAssembly. These figures are the decode's Go heap growth with no intervening collection; they exclude the already loaded asset and the host runtime.

## Verification

- `go test ./audio` covers accepted fixed and variable blocking, skipped metadata, full-scale samples, a missing MD5 signature, every rejection above, and the Beacon asset's exact round trip against its score.
- `go test ./audio -run '^$' -fuzz FuzzDecodeFLAC` fuzzes the decoder and checks that an error returns a zero clip and a success returns an even, nonempty sample count within 64 seconds.
- `go test ./audio -run '^$' -bench DecodeFLACBeaconMusic -benchmem` and the same command with `GOOS=js GOARCH=wasm` and `-exec="$(go env GOROOT)/lib/wasm/go_js_wasm_exec"` measure decode time and allocation.

## References

- [RFC 0019](../rfcs/0019-m18-live-matches-and-distribution.md)
- [RFC 9639, FLAC](https://www.rfc-editor.org/rfc/rfc9639.html)
- [Music specification](music-m17.md)
- [Audio mixer specification](audio-mixer-m15.md)
