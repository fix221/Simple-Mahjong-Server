package game

type KongRecord struct {
	Seat   int
	Kind   string // an | ming | jia
	From   int
	TileID int
}

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
			// others pay 1 total; integer: next seat pays 1
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

func TotalMultiplier(horseFan, patternMul int) int {
	if patternMul <= 0 {
		patternMul = 1
	}
	if horseFan < 0 {
		horseFan = 0
	}
	return horseFan * patternMul
}