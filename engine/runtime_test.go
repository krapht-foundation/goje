package engine_test

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"testing"

	"github.com/krapht-foundation/goje/engine"
)

type testScene struct {
	enter  func() error
	update func(engine.Tick) (engine.Transition, error)
	draw   func(engine.Canvas)
	exit   func()
}

func (s *testScene) Enter() error {
	if s.enter != nil {
		return s.enter()
	}
	return nil
}

func (s *testScene) Update(tick engine.Tick) (engine.Transition, error) {
	if s.update != nil {
		return s.update(tick)
	}
	return engine.Transition{}, nil
}

func (s *testScene) Draw(canvas engine.Canvas) {
	if s.draw != nil {
		s.draw(canvas)
	}
}

func (s *testScene) Exit() {
	if s.exit != nil {
		s.exit()
	}
}

type soundLog []string

func (s *soundLog) Play(cue int, gain, pan float32) {
	*s = append(*s, fmt.Sprintf("play %d %v %v", cue, gain, pan))
}

func (s *soundLog) Clear() { *s = append(*s, "clear") }

func TestTickSoundReachesNestedRuntime(t *testing.T) {
	var log soundLog
	inner, err := engine.Start(func() engine.Scene {
		return &testScene{update: func(tick engine.Tick) (engine.Transition, error) {
			tick.PlaySound(1)
			return engine.Transition{}, nil
		}}
	})
	if err != nil {
		t.Fatal(err)
	}
	outer, err := engine.Start(func() engine.Scene {
		return &testScene{update: func(tick engine.Tick) (engine.Transition, error) {
			inner.SetSoundOutput(tick.Sound())
			if stepErr := inner.Step(engine.Input{}); stepErr != nil {
				return engine.Transition{}, stepErr
			}
			tick.PlaySoundAt(2, 0.5, -1)
			return engine.Transition{}, inner.Stop()
		}}
	})
	if err != nil {
		t.Fatal(err)
	}
	outer.SetSoundOutput(&log)
	if err := outer.Step(engine.Input{}); err != nil {
		t.Fatal(err)
	}
	if want := []string{"play 1 1 0", "play 2 0.5 -1", "clear"}; !slices.Equal(log, want) {
		t.Fatalf("sound requests %q, want %q", log, want)
	}
}

type musicLog struct{ soundLog }

func (m *musicLog) PlayMusic(track int, gain float32, fadeMS int) {
	m.soundLog = append(m.soundLog, fmt.Sprintf("music %d %v %d", track, gain, fadeMS))
}

func (m *musicLog) StopMusic(fadeMS int) {
	m.soundLog = append(m.soundLog, fmt.Sprintf("stop music %d", fadeMS))
}

func (m *musicLog) SetMusicGain(gain float32, fadeMS int) {
	m.soundLog = append(m.soundLog, fmt.Sprintf("music gain %v %d", gain, fadeMS))
}

func TestTickMusicReachesMusicOutputOnly(t *testing.T) {
	update := func(tick engine.Tick) (engine.Transition, error) {
		tick.PlayMusic(0, 0.5, 200)
		tick.SetMusicGain(0.25, 100)
		tick.StopMusic(300)
		return engine.Transition{}, nil
	}
	r, err := engine.Start(func() engine.Scene { return &testScene{update: update} })
	if err != nil {
		t.Fatal(err)
	}
	var plain soundLog
	r.SetSoundOutput(&plain)
	if err := r.Step(engine.Input{}); err != nil || len(plain) != 0 {
		t.Fatalf("an output without music got %q, %v", plain, err)
	}
	var log musicLog
	r.SetSoundOutput(&log)
	if err := r.Step(engine.Input{}); err != nil {
		t.Fatal(err)
	}
	if want := []string{
		"music 0 0.5 200",
		"music gain 0.25 100",
		"stop music 300",
	}; !slices.Equal(
		log.soundLog,
		want,
	) {
		t.Fatalf("music requests %q, want %q", log.soundLog, want)
	}
	r.SetSoundOutput(nil)
	if err := r.Step(engine.Input{}); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeTransitions(t *testing.T) {
	var calls []string
	makeScene := func(name string, transition func(engine.Tick) engine.Transition) engine.Scene {
		calls = append(calls, "create "+name)
		return &testScene{
			enter: func() error { calls = append(calls, "enter "+name); return nil },
			update: func(tick engine.Tick) (engine.Transition, error) {
				calls = append(calls, fmt.Sprintf("tick %d %s", tick.Number, name))
				return transition(tick), nil
			},
			draw: func(engine.Canvas) { calls = append(calls, "draw "+name) },
			exit: func() { calls = append(calls, "exit "+name) },
		}
	}
	var runs int
	r, err := engine.Start(func() engine.Scene {
		runs++
		return makeScene(fmt.Sprintf("A%d", runs), func(tick engine.Tick) engine.Transition {
			if tick.Input.Pressed.Has(engine.KeyR) {
				return engine.Restart()
			}
			return engine.Replace(func() engine.Scene {
				return makeScene("B", func(engine.Tick) engine.Transition { return engine.Transition{} })
			})
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []engine.Input{{Pressed: engine.KeyR}, {}, {}} {
		if err := r.Step(input); err != nil {
			t.Fatal(err)
		}
		if err := r.Draw(nil); err != nil {
			t.Fatal(err)
		}
	}
	for range 2 {
		if err := r.Stop(); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{
		"create A1", "enter A1", "tick 1 A1", "exit A1",
		"create A2", "enter A2", "draw A2", "tick 2 A2", "exit A2",
		"create B", "enter B", "draw B", "tick 3 B", "draw B", "exit B",
	}
	if !slices.Equal(calls, want) {
		t.Fatalf("lifecycle calls = %v; want %v", calls, want)
	}
	if err := r.Step(engine.Input{}); !errors.Is(err, engine.ErrRuntimeStopped) {
		t.Fatalf("step after stop = %v; want ErrRuntimeStopped", err)
	}
}

func TestRuntimeLogsTransitionsButNotRoutineTicks(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&output, nil))
	runtime, err := engine.StartWithLogger(t.Context(), func() engine.Scene {
		return &testScene{update: func(tick engine.Tick) (engine.Transition, error) {
			switch tick.Number {
			case 2:
				return engine.Restart(), nil
			case 3:
				return engine.Replace(func() engine.Scene { return &testScene{} }), nil
			default:
				return engine.Transition{}, nil
			}
		}}
	}, logger)
	if err != nil {
		t.Fatal(err)
	}
	for range 4 {
		if err := runtime.Step(engine.Input{}); err != nil {
			t.Fatal(err)
		}
	}
	if err := runtime.Stop(); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], "action=restart tick=2") ||
		!strings.Contains(lines[1], "action=replace tick=3") {
		t.Fatalf("expected one log per scene transition, got:\n%s", output.String())
	}
}

func TestRuntimeFailureCleanup(t *testing.T) {
	failure := errors.New("scene failure")
	for _, phase := range []string{"start", "update", "replace"} {
		t.Run(phase, func(t *testing.T) {
			var activeExits, failedExits int
			failedScene := func() engine.Scene {
				return &testScene{
					enter: func() error { return failure },
					exit:  func() { failedExits++ },
				}
			}
			if phase == "start" {
				r, err := engine.Start(failedScene)
				if r != nil || !errors.Is(err, failure) || failedExits != 0 {
					t.Fatalf("failed start: runtime=%v error=%v exits=%d", r, err, failedExits)
				}
				return
			}
			r, err := engine.Start(func() engine.Scene {
				return &testScene{
					update: func(engine.Tick) (engine.Transition, error) {
						if phase == "update" {
							return engine.Replace(func() engine.Scene {
								t.Fatal("failed update must not create a replacement")
								return nil
							}), failure
						}
						return engine.Replace(failedScene), nil
					},
					exit: func() { activeExits++ },
				}
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := r.Step(engine.Input{}); !errors.Is(err, failure) {
				t.Fatalf("step = %v; want scene failure", err)
			}
			if err := r.Step(engine.Input{}); !errors.Is(err, engine.ErrRuntimeStopped) {
				t.Fatalf("step after failure = %v; want ErrRuntimeStopped", err)
			}
			if err := r.Stop(); err != nil {
				t.Fatal(err)
			}
			if activeExits != 1 || failedExits != 0 {
				t.Fatalf("exit counts = (%d, %d); want (1, 0)", activeExits, failedExits)
			}
		})
	}
}

func TestRuntimeRejectsRecursiveLifecycleCalls(t *testing.T) {
	var r *engine.Runtime
	var err error
	r, err = engine.Start(func() engine.Scene {
		return &testScene{
			update: func(engine.Tick) (engine.Transition, error) {
				if stepErr := r.Step(engine.Input{}); !errors.Is(stepErr, engine.ErrRuntimeBusy) {
					t.Fatalf("recursive step = %v; want ErrRuntimeBusy", stepErr)
				}
				if stopErr := r.Stop(); !errors.Is(stopErr, engine.ErrRuntimeBusy) {
					t.Fatalf("recursive stop = %v; want ErrRuntimeBusy", stopErr)
				}
				return engine.Transition{}, nil
			},
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Step(engine.Input{}); err != nil {
		t.Fatal(err)
	}
	if err := r.Stop(); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeSteadyStateAllocations(t *testing.T) {
	r, err := engine.Start(func() engine.Scene { return &testScene{} })
	if err != nil {
		t.Fatal(err)
	}
	allocs := testing.AllocsPerRun(1000, func() {
		if err := r.Step(engine.Input{}); err != nil {
			t.Fatal(err)
		}
	})
	if allocs != 0 {
		t.Fatalf("no-op steps allocated %g times per tick; want zero", allocs)
	}
	if err := r.Stop(); err != nil {
		t.Fatal(err)
	}
}
