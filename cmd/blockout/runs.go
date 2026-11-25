package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/codezerg/blockout-ai-game-assets/internal/layout"
	"github.com/codezerg/blockout-ai-game-assets/internal/measure"
)

func readRunInfo(dir string) (runInfo, error) {
	var info runInfo
	data, err := os.ReadFile(filepath.Join(dir, "run.json"))
	if err != nil {
		return info, err
	}
	err = json.Unmarshal(data, &info)
	return info, err
}

type runEntry struct {
	Dir       string           `json:"dir"`
	Name      string           `json:"name"`
	Run       string           `json:"run"`
	Tag       string           `json:"tag,omitempty"`
	Status    string           `json:"status"`
	Kind      string           `json:"kind"`
	Model     string           `json:"model"`
	Size      string           `json:"size"`
	Sketch    string           `json:"sketch,omitempty"`
	Scale     float64          `json:"sketch_scale,omitempty"`
	CostUSD   *float64         `json:"cost_usd"`
	Attempts  int              `json:"attempts,omitempty"`
	DurationS float64          `json:"duration_s,omitempty"`
	Started   time.Time        `json:"started"`
	Error     string           `json:"error,omitempty"`
	Scores    *measure.Summary `json:"scores,omitempty"`
}

// group aggregates the runs of one variant: same tag, layout, model, size
// and sketch style.
type group struct {
	Tag        string  `json:"tag,omitempty"`
	Name       string  `json:"name"`
	Model      string  `json:"model"`
	Size       string  `json:"size"`
	Sketch     string  `json:"sketch,omitempty"`
	Scale      float64 `json:"sketch_scale,omitempty"`
	Runs       int     `json:"runs"`
	OK         int     `json:"ok"`
	Failed     int     `json:"failed"`
	CostUSD    float64 `json:"cost_usd"`
	Scored     int     `json:"scored"`
	Pieces     int     `json:"pieces"`       // over scored runs
	InsideRate float64 `json:"inside_rate"`  // pieces within box + margin
	TouchEdge  int     `json:"touches_edge"` // pieces drawn up to the sheet's edge
	MeanIoU    float64 `json:"mean_iou"`
	MinIoU     float64 `json:"min_iou"`
	MaxSpill   int     `json:"max_spill"`
	MeanRatio  float64 `json:"mean_size_ratio"`
	MaxLeak    float64 `json:"max_leak"`
	Missing    int     `json:"missing"` // pieces not found
	Merged     int     `json:"merged"`
	Stray      int     `json:"stray"`
}

func (c *cli) runs(args []string) int {
	fs := flag.NewFlagSet("runs", flag.ContinueOnError)
	root := fs.String("out", "out", "directory that holds run directories")
	tag := fs.String("tag", "", "only runs with this tag")
	name := fs.String("name", "", "only runs of this layout name")
	pos, err := parse(fs, args)
	if err != nil {
		return c.usage(err)
	}
	if len(pos) != 0 {
		return c.usage(errors.New("runs takes no positional arguments"))
	}
	entries, err := listRuns(*root)
	if err != nil {
		return c.internal(err)
	}
	var keep []runEntry
	for _, e := range entries {
		if (*tag == "" || e.Tag == *tag) && (*name == "" || e.Name == *name) {
			keep = append(keep, e)
		}
	}

	groups := map[string]*group{}
	var order []string
	total := 0.0
	for _, e := range keep {
		key := fmt.Sprintf("%s|%s|%s|%s|%s|%v", e.Tag, e.Name, e.Model, e.Size, e.Sketch, e.Scale)
		g, ok := groups[key]
		if !ok {
			g = &group{Tag: e.Tag, Name: e.Name, Model: e.Model, Size: e.Size, Sketch: e.Sketch, Scale: e.Scale, MinIoU: 1}
			groups[key] = g
			order = append(order, key)
		}
		g.Runs++
		switch e.Status {
		case "ok":
			g.OK++
		case "failed":
			g.Failed++
		}
		if e.CostUSD != nil {
			g.CostUSD += *e.CostUSD
			total += *e.CostUSD
		}
		if s := e.Scores; s != nil {
			g.Scored++
			g.Pieces += s.Pieces
			g.InsideRate += float64(s.Inside)
			g.TouchEdge += s.TouchEdge
			g.MeanIoU += s.MeanIoU
			g.MinIoU = min(g.MinIoU, s.MinIoU)
			g.MaxSpill = max(g.MaxSpill, s.MaxSpill)
			g.MeanRatio += s.MeanRatio
			g.MaxLeak = max(g.MaxLeak, s.MaxLeak)
			g.Missing += s.Pieces - s.Found
			g.Merged += s.Merged
			g.Stray += s.Stray
		}
	}
	out := make([]*group, 0, len(order))
	for _, k := range order {
		g := groups[k]
		g.CostUSD = round(g.CostUSD, 4)
		if g.Pieces > 0 {
			g.InsideRate = round(g.InsideRate/float64(g.Pieces), 3)
		}
		if g.Scored > 0 {
			g.MeanIoU = round(g.MeanIoU/float64(g.Scored), 3)
			g.MeanRatio = round(g.MeanRatio/float64(g.Scored), 3)
		} else {
			g.MinIoU = 0
		}
		out = append(out, g)
	}
	if keep == nil {
		keep = []runEntry{}
	}
	c.emit(map[string]any{"ok": true, "runs": keep, "groups": out, "total_cost_usd": round(total, 4)})
	return exitOK
}

// listRuns reads every run directory under root, oldest first.
func listRuns(root string) ([]runEntry, error) {
	des, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []runEntry
	for _, de := range des {
		if !de.IsDir() || !runDirPattern.MatchString(de.Name()) {
			continue
		}
		dir := filepath.Join(root, de.Name())
		info, err := readRunInfo(dir)
		if err != nil {
			continue
		}
		e := runEntry{
			Dir: filepath.ToSlash(dir), Name: info.Name, Run: info.Run, Tag: info.Tag, Status: info.Status,
			Kind: info.Kind, Model: info.Model, Size: info.Size, Sketch: info.Sketch, Scale: info.SketchScale, CostUSD: info.CostUSD,
			Attempts: info.Attempts, DurationS: info.DurationS, Started: info.Started,
		}
		if e.Kind == "sheet" && e.Sketch == "" {
			e.Sketch = layout.SketchBlocks // runs from before sketch styles
		}
		if info.Error != nil {
			e.Error = info.Error.Code
		}
		if data, err := os.ReadFile(filepath.Join(dir, "scores.json")); err == nil {
			var sc measure.Scores
			if json.Unmarshal(data, &sc) == nil {
				e.Scores = &sc.Summary
			}
		}
		out = append(out, e)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Started.Before(out[j].Started) })
	return out, nil
}

// tagSpend sums the cost of runs with a tag. Failed runs cost nothing.
func tagSpend(root, tag string) float64 {
	entries, _ := listRuns(root)
	total := 0.0
	for _, e := range entries {
		if e.Tag == tag && e.CostUSD != nil {
			total += *e.CostUSD
		}
	}
	return total
}
