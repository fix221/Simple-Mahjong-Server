package game

// QueCleared 定缺后：手牌+副露中不能再有定缺花色的牌
func QueCleared(hand []Tile, melds []Meld, que Suit) bool {
	if que < Wan || que > Tiao {
		return false
	}
	for _, t := range hand {
		if t.Suit == que {
			return false
		}
	}
	for _, m := range melds {
		for _, t := range m.Tiles {
			if t.Suit == que {
				return false
			}
		}
	}
	return true
}

// CanWinSichuan 能胡且已定缺且手牌无定缺花色
func CanWinSichuan(hand []Tile, melds []Meld, que Suit) bool {
	if !CanWin(hand) {
		return false
	}
	return QueCleared(hand, melds, que)
}
