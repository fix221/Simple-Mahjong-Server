package universal

import (
	"encoding/json"
	"fmt"
	"math"
)

// ParseTile accepts Godot/JS float numbers in JSON
func ParseTile(raw json.RawMessage) (Tile, error) {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		var t Tile
		if err2 := json.Unmarshal(raw, &t); err2 == nil {
			return t, nil
		}
		return Tile{}, err
	}
	return TileFromMap(m)
}

func TileFromMap(m map[string]any) (Tile, error) {
	suit, ok1 := toInt(m["suit"])
	num, ok2 := toInt(m["num"])
	if !ok1 || !ok2 {
		return Tile{}, fmt.Errorf("invalid tile: %v", m)
	}
	return Tile{Suit: Suit(suit), Num: num}, nil
}

func ParseTiles(raw json.RawMessage) ([]Tile, error) {
	var arr []map[string]any
	if err := json.Unmarshal(raw, &arr); err != nil {
		var tiles []Tile
		if err2 := json.Unmarshal(raw, &tiles); err2 == nil {
			return tiles, nil
		}
		return nil, err
	}
	out := make([]Tile, 0, len(arr))
	for _, m := range arr {
		t, err := TileFromMap(m)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

func toInt(v any) (int, bool) {
	switch x := v.(type) {
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return 0, false
		}
		return int(math.Round(x)), true
	case float32:
		return int(math.Round(float64(x))), true
	case int:
		return x, true
	case int64:
		return int(x), true
	case json.Number:
		f, err := x.Float64()
		if err != nil {
			return 0, false
		}
		return int(math.Round(f)), true
	default:
		return 0, false
	}
}