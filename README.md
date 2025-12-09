![](https://github.com/A-zrael/Forza-Horizon-5-Telemetry-Viewer/blob/main/Telemetry%20Viewer%20Icon.jpeg?raw=true)

# 🏁 Forza Telemetry Viewer
> Turn raw FH5 / Forza Motorsport CSV exports into a cinematic lap explorer with zero setup.
> Built to pair with the capture flow from [Forza-Horizon-5-Recorder](https://github.com/A-zrael/Forza-Horizon-5-Recorder).

## Why this is fun
- ⚡️ Drop in one or many CSVs; laps, sectors, and race type are auto-detected.
- 🧭 Builds a smoothed “master” lap and maps every car to it for side-by-side overlays.
- 🚨 Event radar: crashes, resets, collisions, rumble/puddle hits, drifts, traction loss, and position changes.
- 🔥 Heatmaps for acceleration + surface grip, plus corner/segment entry–apex–exit speeds and per-lap splits.
- 🖥️ Local web viewer (`web/index.html`) with map, timeline scrubber, live readouts, and filterable events/legends.

## 30‑second launch
```bash
# Inside GoInterpreter-fh5
go run . -file path/to/session.csv
# or compare multiple runs:
go run . -file path/to/car1.csv -file path/to/car2.csv
# or point at a folder (recursive):
go run . -folder telemetry/
```
- By default it writes `web/data.json` and serves the viewer at `http://localhost:8080`.
- Add `-out results.json` to export only, or `-serve=false` to skip hosting the UI.

## Data it expects
The loader is forgiving but needs these columns (case-insensitive) in your CSV:
`timestampms`, `speed_mps`, `accel_x`, `accel_y`, `accel_z`, `vel_x`, `vel_y`, `vel_z`.

Highly recommended for richer visuals: `speed_kph`/`speed_mph`, `pos_x`/`pos_y`/`pos_z`, `gear`, `brake`, `accel` (throttle), `steer`, tire slip/temps, rumble/puddle flags, and race position.

## Flags that matter
- `-lap-length` / `-lap-count` / `-lap-tol` / `-min-lap-spacing` / `-start-finish-radius` — tune lap detection when the start/finish is tricky.
- `-master-samples` — points used to build the averaged master lap (default 4000).
- `-use-master=false` — emit per-lap raw points instead of the averaged trace.
- `-sprint` — treat the run as a point-to-point (no start/finish crossing).
- `-addr :8080` — change the local viewer port.

## What you’ll see in the viewer
- Track map with per-car colors, master lap outline, and acceleration/traction heatmap overlay.
- Scrubbable timeline with live speed, delta vs. best, steering/brake/throttle, gear, and lap/split info.
- Event filters + presets to spotlight crashes, drifts, puddles, traction loss, position gains/losses, and overtakes.
- Corner and segment tables with entry/min/exit speeds (mph + km/h) and per-lap comparisons.

## Outputs
- `web/data.json` — everything the viewer needs (master track, per-car traces, events, stats).
- `stdout` — JSON payload when `-serve=false -out` is omitted; useful for piping into other tools.

## Tips
- If lap detection is noisy on tight tracks, bump `-start-finish-radius` to ~15–20 or set an explicit `-lap-count`.
- Feeding multiple cars lets the viewer detect overtakes and visualize deltas on the shared master lap.
- The pipeline drops pre- and post-race zeroed samples automatically—feed it the raw game dump. 
