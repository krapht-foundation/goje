# Beacon challenge, M18

Beacon is the playable fixture for the M18 distribution features. [RFC 0019](../rfcs/0019-m18-live-matches-and-distribution.md) asks for a short challenge on a fixed board with a clear win, a timeout, and a restart, playable by keyboard and by touch. The same source and assets build for native desktop and the web. The game needs no network or update check to start and lives in [`examples/beacon`](../examples/beacon).

## Board

The board is the level in `assets/arena.json`, drawn in a 160 × 128 logical viewport. Cell (c, r) covers logical x from 16 + 32c to 47 + 32c and y from 16 + 32r to 47 + 32r. Every placement must have the kind `beacon`, and there must be at least one. The shipped level has beacons at (0, 0), (2, 0), and (0, 1). The level editor smoke moves the first beacon to (1, 0), so the authored fixture has beacons at (1, 0), (2, 0), and (0, 1).

An unlit beacon is a 12 × 12 square of (91, 141, 209) centered in its cell, from x 26 + 32c to 37 + 32c and y 26 + 32r to 37 + 32r. A lit beacon is the same square in (248, 190, 66). The unlit color appears nowhere else on screen.

## Rules

- A round starts in the ready state. The clock does not run until the first action: a primary press anywhere, Space, or a held movement key. An unattended page therefore cannot lose.
- A primary press, which is a mouse click or the first finger's touch, lights the unlit beacon whose cell contains it.
- Space lights every unlit beacon in the board column above the tomato's anchor. The tomato walks the lane below the board with the arrow keys or WASD, one logical pixel per tick.
- A beacon stays lit for the rest of the round. Each tick that lights at least one beacon plays the toggle cue once.
- The round is won on the tick that lights the last beacon. Otherwise it times out when the clock reaches 600 ticks, which is 10 seconds at 60 Hz. Lighting the last beacon on the final tick wins.
- After a win or a timeout, the game ignores movement and lighting. A primary press anywhere, Space, Enter, or R restarts the round. R also restarts a round that is still running.
- A restart creates a fresh scene from the same board: every beacon unlit, a full clock, and the tomato standing at the lane start.

The rules depend only on tick input, so the result of a given input sequence is the same on every host and does not depend on audio.

## Display

- While the round is ready or running, a timer bar fills logical x 16 to 143 and y 6 to 9. The remaining time is (244, 241, 235) and the elapsed time is (52, 69, 87). The bar is full in the ready state.
- A won round replaces the bar with a banner from x 16 to 143 and y 2 to 13 in (248, 190, 66), with the text `ALL LIT! TAP OR R`.
- A timed-out round shows the same banner in (215, 84, 73) with the text `TIME UP! TAP OR R`.
- Logical pixel (17, 3) is (20, 28, 41) during a round, the win color after a win, and the timeout color after a timeout. The pixel checks use it to read the result.

The native window title starts with `Goje Beacon`, which the Windows package smoke relies on.

## Music

`assets/music.flac` is decoded at startup with `audio.DecodeFLAC` and passed as music track 0. The first update of every round requests the track at gain 0.5 with a 500 ms fade. The first request starts the loop with a fade in, and later requests for the same track only move its gain. A win or a timeout fades the gain to 0.2 over 500 ms. A restart fades it back to 0.5 without restarting the track. In the browser the request waits for the first trusted gesture that unlocks audio.

## Verification

- `go test ./examples/beacon` plays the shipped board to a keyboard win, a touch win, and a timeout, checks that the clock waits for the first action, and checks each restart path.
- The native smoke build (`goje_smoke,goje_beacon_smoke`) with no argument reads the presented framebuffer of the authored fixture. It checks the board and the full timer, the idle blink, the hop, a pointer light, R, a keyboard win with its banner, a Space restart, a timeout with its banner, and a restart from a press on the lane. It runs the timeout at ten game ticks per host tick so that the 600-tick clock expires in about one second.
- `--audio` compares every device buffer with a reference `audio.Mixer` to which the expected requests are applied in order. Those requests are the music fade in, the overlapping cues, the win cue with the fade down, and the restart's fade back up. A fade that differs by one millisecond fails the check.
- `scripts/smoke-beacon.mjs play` and `build` repeat the visual cases in Chromium with a paused page clock. They add a second page with a touch screen that wins with three taps and restarts with a fourth.
- `scripts/smoke-beacon-audio.mjs` checks that the music is one looping 48 kHz buffer that starts once after unlock and fades to 0.5, then to 0.2 on a win, then to 0.5 on restart. It keeps that buffer out of the cue capture, so the cue checks are unchanged.
