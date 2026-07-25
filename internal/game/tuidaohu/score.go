package tuidaohu

import "mahjong/internal/game/universal"

// KongRecord: per-hand kong fee record (settle at end)
// Kind: an | ming | jia
type KongRecord struct {
	Seat   int
	Kind   string
	From   int
	TileID int
}

// ScoreKongFees returns net delta per seat.
// ming: donor pays 3; an: each other pays 2; jia: others pay 1 total (next seat pays 1)
func ScoreKongFees(n int, recs []KongRecord) []int {
	delta := make([]int, n)
	if n <= 1 {
		return delta
	}
	for _, r := range recs {
		if r.Seat < 0 || r.Seat >= n {
			continue
		}
		switch r.Kind {
		case "ming":
			if r.From >= 0 && r.From < n && r.From != r.Seat {
				delta[r.From] -= 3
				delta[r.Seat] += 3
			}
		case "an":
			for i := 0; i < n; i++ {
				if i == r.Seat {
					continue
				}
				delta[i] -= 2
				delta[r.Seat] += 2
			}
		case "jia":
			payer := (r.Seat + 1) % n
			for k := 0; k < n && payer == r.Seat; k++ {
				payer = (payer + 1) % n
			}
			if payer != r.Seat {
				delta[payer] -= 1
				delta[r.Seat] += 1
			}
		}
	}
	return delta
}

func IsValidHorse(t universal.Tile) bool {
	if t.Suit > universal.Tiao {
		return false
	}
	return t.Num == 1 || t.Num == 5 || t.Num == 9
}

func CountValidHorses(tiles []universal.Tile) int {
	n := 0
	for _, t := range tiles {
		if IsValidHorse(t) {
			n++
		}
	}
	return n
}

func TotalMultiplier(horseFan, patternMul int) int {
	if patternMul <= 0 {
		patternMul = 1
	}
	if horseFan < 0 {
		horseFan = 0
	}
	return horseFan * patternMul
}