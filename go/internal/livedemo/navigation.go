package livedemo

import (
	_ "embed"
	"encoding/json"
	"errors"
	"math"
	"slices"
	"time"
)

type (
	Point [3]float64
	Route struct {
		Points []Point
		Length float64
	}
)

type (
	TravelRequest struct{ ID, From, To, Kind string }
	Travel        struct {
		ID        string
		StartedAt time.Time
		Duration  time.Duration
	}
)

type routeLayout struct {
	Version   string                      `json:"version"`
	WalkSpeed float64                     `json:"walk_speed"`
	Stations  map[string]map[string]Point `json:"stations"`
	Nodes     []Point                     `json:"nodes"`
	Edges     [][2]int                    `json:"edges"`
	Parity    []struct {
		Kind       string  `json:"kind"`
		From       string  `json:"from"`
		To         string  `json:"to"`
		Length     float64 `json:"length"`
		DurationMS int64   `json:"duration_ms"`
	} `json:"parity"`
}

//go:embed work-layout.json
var layoutJSON []byte

var layout = func() routeLayout {
	var out routeLayout
	if err := json.Unmarshal(layoutJSON, &out); err != nil {
		panic(err)
	}
	return out
}()

func distance(a, b Point) float64 {
	dx, dy, dz := b[0]-a[0], b[1]-a[1], b[2]-a[2]
	return math.Sqrt(dx*dx + dy*dy + dz*dz)
}

func RouteFor(from, to, kind string) (Route, error) {
	stations := layout.Stations[kind]
	origin, ok := stations[from]
	target, valid := stations[to]
	if !ok || !valid || layout.WalkSpeed <= 0 {
		return Route{}, errors.New("invalid public work layout")
	}
	if distance(origin, target) < 1e-6 {
		return Route{Points: []Point{origin}}, nil
	}
	nodes := slices.Clone(layout.Nodes)
	nearest := layout.Edges[0]
	snapped := nodes[nearest[0]]
	best := math.Inf(1)
	for _, edge := range layout.Edges {
		a, b := nodes[edge[0]], nodes[edge[1]]
		squared := math.Pow(distance(a, b), 2)
		dot := 0.0
		for i := range 3 {
			dot += (origin[i] - a[i]) * (b[i] - a[i])
		}
		ratio := math.Max(0, math.Min(1, dot/squared))
		var p Point
		for i := range 3 {
			p[i] = a[i] + (b[i]-a[i])*ratio
		}
		if d := distance(origin, p); d < best {
			best = d
			snapped = p
			nearest = edge
		}
	}
	start := len(nodes)
	nodes = append(nodes, snapped)
	goal := -1
	for i, p := range nodes {
		if distance(p, target) < 1e-6 {
			goal = i
			break
		}
	}
	if goal < 0 {
		return Route{}, errors.New("disconnected work bay")
	}
	neighbours := make([][]int, len(nodes))
	edges := append(slices.Clone(layout.Edges), [2]int{start, nearest[0]}, [2]int{start, nearest[1]})
	for _, e := range edges {
		neighbours[e[0]] = append(neighbours[e[0]], e[1])
		neighbours[e[1]] = append(neighbours[e[1]], e[0])
	}
	costs := make([]float64, len(nodes))
	previous := make([]int, len(nodes))
	open := make([]bool, len(nodes))
	for i := range nodes {
		costs[i] = math.Inf(1)
		previous[i] = -1
		open[i] = true
	}
	costs[start] = 0
	for {
		current := -1
		for i := range nodes {
			if open[i] && (current < 0 || costs[i] < costs[current]) {
				current = i
			}
		}
		if current < 0 || current == goal {
			break
		}
		open[current] = false
		for _, next := range neighbours[current] {
			cost := costs[current] + distance(nodes[current], nodes[next])
			if cost < costs[next] {
				costs[next] = cost
				previous[next] = current
			}
		}
	}
	if math.IsInf(costs[goal], 1) {
		return Route{}, errors.New("disconnected work route")
	}
	points := []Point{}
	for i := goal; i >= 0; i = previous[i] {
		points = append(points, nodes[i])
	}
	slices.Reverse(points)
	if distance(origin, points[0]) > 1e-6 {
		points = append([]Point{origin}, points...)
	} else {
		points[0] = origin
	}
	route := Route{}
	for _, p := range points {
		if len(route.Points) == 0 || distance(p, route.Points[len(route.Points)-1]) > 1e-6 {
			route.Points = append(route.Points, p)
		}
	}
	for i := 1; i < len(route.Points); i++ {
		route.Length += distance(route.Points[i-1], route.Points[i])
	}
	return route, nil
}

func TravelDuration(route Route) time.Duration {
	return time.Duration(math.Ceil(route.Length/layout.WalkSpeed*1000)) * time.Millisecond
}

// Whole-route windows conservatively serialize conflicting movement. This small
// demo needs no browser acknowledgement and cannot deadlock across crossings.
func ScheduleTravel(requests []TravelRequest, start time.Time) ([]Travel, error) {
	requests = slices.Clone(requests)
	slices.SortFunc(requests, func(a, b TravelRequest) int {
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})
	out := []Travel{}
	next := start
	for _, r := range requests {
		route, err := RouteFor(r.From, r.To, r.Kind)
		if err != nil {
			return nil, err
		}
		duration := TravelDuration(route)
		out = append(out, Travel{ID: r.ID, StartedAt: next, Duration: duration})
		next = next.Add(duration)
	}
	return out, nil
}
