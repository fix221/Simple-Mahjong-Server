package main

import (
	"encoding/json"
	"log"
	"math/rand"
	"net/http"
	"sync"
	"time"

	"mahjong/internal/game"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

type Msg struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
}

type Player struct {
	ID     string          `json:"id"`
	Name   string          `json:"name"`
	Gold   int             `json:"gold"`
	Online bool            `json:"online"`
	Conn   *websocket.Conn `json:"-"`
}

type Room struct {
	ID      string    `json:"id"`
	Rule    string    `json:"rule"`
	Owner   string    `json:"owner"`
	InGame  bool      `json:"in_game"`
	Players []*Player `json:"players"`
	State   *State    `json:"-"`
}

type State struct {
	Rule      string
	Wall      []game.Tile
	Hands     [][]game.Tile
	Discards  [][]game.Tile
	Melds     [][]game.Meld
	Current   int
	Phase     string // exchange | dingque | discard | wait_action
	LastFrom  int
	LastTile  game.Tile
	Won       []bool
	Passed    []bool
	NeedAct   []bool
	Exchange  [][]game.Tile
	Exchanged []bool
	Que       []int  // 定缺花色 0万1筒2条，-1未选
	QueDone   []bool
}

type Hub struct {
	mu    sync.Mutex
	rooms map[string]*Room
}

var hub = &Hub{rooms: map[string]*Room{}}

// 按昵称记金币：每个名称默认 10000（进程内，重启清零）
const defaultGold = 10000
var (
	nameGoldMu sync.Mutex
	nameGold   = map[string]int{}
)

func goldForName(name string) int {
	nameGoldMu.Lock()
	defer nameGoldMu.Unlock()
	if name == "" {
		return defaultGold
	}
	if g, ok := nameGold[name]; ok {
		return g
	}
	nameGold[name] = defaultGold
	return defaultGold
}

func setGoldForName(name string, g int) {
	nameGoldMu.Lock()
	defer nameGoldMu.Unlock()
	if name == "" {
		return
	}
	if g < 0 {
		g = 0
	}
	nameGold[name] = g
}

func main() {
	rand.Seed(time.Now().UnixNano())
	http.HandleFunc("/ws", onWS)
	http.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"ok":true}`))
	})
	log.Println("Mahjong server :8081")
	log.Fatal(http.ListenAndServe(":8081", nil))
}

func onWS(w http.ResponseWriter, r *http.Request) {
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	// 长连接：读超时 + pong 续期；定时 ping
	const pongWait = 90 * time.Second
	const pingPeriod = 30 * time.Second
	c.SetReadLimit(1 << 20)
	_ = c.SetReadDeadline(time.Now().Add(pongWait))
	c.SetPongHandler(func(string) error {
		return c.SetReadDeadline(time.Now().Add(pongWait))
	})

	// session 可在 resume 后切换为房间内原玩家指针
	session := &Player{ID: id6(), Conn: c, Name: "玩家", Online: true, Gold: defaultGold}
	reply(c, "connected", map[string]any{"id": session.ID, "resume_hint": true, "gold": session.Gold})

	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(pingPeriod)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := c.WriteControl(websocket.PingMessage, []byte("ping"), time.Now().Add(5*time.Second)); err != nil {
					return
				}
			case <-done:
				return
			}
		}
	}()

	defer func() {
		close(done)
		onLeave(session)
	}()

	for {
		_, b, err := c.ReadMessage()
		if err != nil {
			return
		}
		_ = c.SetReadDeadline(time.Now().Add(pongWait))
		var m Msg
		if json.Unmarshal(b, &m) != nil {
			continue
		}
		if m.Type == "ping" {
			reply(c, "pong", map[string]any{"t": time.Now().Unix()})
			continue
		}
		if m.Type == "resume" {
			if np := tryResume(session, c, m.Data); np != nil {
				session = np
			}
			continue
		}
		handle(session, m)
	}
}

func handle(p *Player, m Msg) {
	switch m.Type {
	case "set_name":
		var d struct {
			Name string `json:"name"`
		}
		json.Unmarshal(m.Data, &d)
		if d.Name != "" {
			p.Name = d.Name
			// 每个名称默认 10000 金币（已有则沿用进程内记录）
			p.Gold = goldForName(p.Name)
			reply(p.Conn, "profile", map[string]any{
				"name": p.Name,
				"gold": p.Gold,
			})
		}
	case "create_room":
		createRoom(p, m.Data)
	case "join_room":
		joinRoom(p, m.Data)
	case "leave_room":
		leaveRoom(p)
	case "list_rooms":
		listRooms(p)
	case "start_game":
		startGame(p)
	case "discard":
		doDiscard(p, m.Data)
	case "action":
		doAction(p, m.Data)
	case "exchange":
		doExchange(p, m.Data)
	case "self_win":
		doSelfWin(p)
	case "dingque":
		doDingQue(p, m.Data)
	}
}


// tryResume 用旧 player_id 恢复离线座位，成功返回房间内玩家指针
func tryResume(temp *Player, c *websocket.Conn, raw json.RawMessage) *Player {
	var d struct {
		PlayerID string `json:"player_id"`
		Name     string `json:"name"`
	}
	if json.Unmarshal(raw, &d) != nil || d.PlayerID == "" {
		reply(c, "error", map[string]string{"msg": "重连参数无效"})
		return nil
	}
	hub.mu.Lock()
	defer hub.mu.Unlock()

	for _, r := range hub.rooms {
		for seat, pl := range r.Players {
			if pl.ID != d.PlayerID {
				continue
			}
			// 已在线且不是同一连接：拒绝顶号或允许顶号——允许顶号
			if pl.Online && pl.Conn != nil && pl.Conn != c {
				_ = pl.Conn.Close()
			}
			pl.Conn = c
			pl.Online = true
			if d.Name != "" {
				pl.Name = d.Name
			}
			// 临时连接只是壳，不进房间
			temp.Online = false
			temp.Conn = nil

			broadcast(r, "player_reconnected", map[string]any{
				"player_id": pl.ID,
				"name":      pl.Name,
				"seat":      seat,
			})
			broadcast(r, "tips", map[string]any{
				"msg": pl.Name + " 已重新连接",
			})
			broadcast(r, "room", roomView(r))

			// 推送完整状态给重连者
			pushResumeState(pl, r, seat)
			return pl
		}
	}
	reply(c, "resume_fail", map[string]any{"msg": "没有可恢复的对局，请重新进房"})
	return nil
}

func pushResumeState(p *Player, r *Room, seat int) {
	data := map[string]any{
		"id":      p.ID,
		"room":    roomView(r),
		"in_game": r.InGame,
		"seat":    seat,
		"rule":    r.Rule,
		"players": names(r),
	}
	if !r.InGame || r.State == nil {
		reply(p.Conn, "resume_ok", data)
		return
	}
	st := r.State
	data["hand"] = st.Hands[seat]
	data["phase"] = st.Phase
	data["current"] = st.Current
	data["wall"] = len(st.Wall)
	data["won"] = st.Won
	if st.Rule == "sichuan" {
		data["que"] = st.Que[seat]
		data["all_que"] = st.Que
		data["need_exchange"] = st.Phase == "exchange" && !st.Exchanged[seat]
		data["need_dingque"] = st.Phase == "dingque" && !st.QueDone[seat]
		data["exchanged"] = st.Exchanged[seat]
	}
	// 弃牌简表
	disc := make([][]game.Tile, len(st.Discards))
	for i := range st.Discards {
		disc[i] = st.Discards[i]
	}
	data["discards"] = disc
	reply(p.Conn, "resume_ok", data)

	// 若轮到他操作，补发提示
	if st.Phase == "discard" && st.Current == seat && !st.Won[seat] {
		reply(p.Conn, "turn", map[string]any{"current": st.Current, "phase": st.Phase, "wall": len(st.Wall)})
	}
	if st.Phase == "wait_action" && st.NeedAct != nil && seat < len(st.NeedAct) && st.NeedAct[seat] && !st.Passed[seat] {
		// 简化：提示可操作，具体按钮由客户端根据状态再请求；补发 action 粗提示
		reply(p.Conn, "tips", map[string]any{"msg": "有人出牌，请查看是否可碰/胡"})
	}
}
func createRoom(p *Player, raw json.RawMessage) {
	var d struct {
		Rule string `json:"rule"`
	}
	json.Unmarshal(raw, &d)
	if d.Rule != "sichuan" {
		d.Rule = "tuidaohu"
	}
	leaveRoom(p)
	hub.mu.Lock()
	defer hub.mu.Unlock()
	r := &Room{ID: id4(), Rule: d.Rule, Owner: p.ID, Players: []*Player{p}}
	hub.rooms[r.ID] = r
	reply(p.Conn, "room", roomView(r))
}

func joinRoom(p *Player, raw json.RawMessage) {
	var d struct {
		RoomID string `json:"room_id"`
	}
	json.Unmarshal(raw, &d)
	hub.mu.Lock()
	defer hub.mu.Unlock()
	r := hub.rooms[d.RoomID]
	if r == nil {
		reply(p.Conn, "error", map[string]string{"msg": "房间不存在"})
		return
	}
	if r.InGame {
		reply(p.Conn, "error", map[string]string{"msg": "已在对局中"})
		return
	}
	if len(r.Players) >= 4 {
		reply(p.Conn, "error", map[string]string{"msg": "房间已满"})
		return
	}
	for _, x := range r.Players {
		if x.ID == p.ID {
			reply(p.Conn, "room", roomView(r))
			return
		}
	}
	removeFromRoomsLocked(p, "leave")
	r.Players = append(r.Players, p)
	broadcast(r, "room", roomView(r))
}

func removeFromRoomsLocked(p *Player, reason string) {
	for id, r := range hub.rooms {
		for i, x := range r.Players {
			if x.ID != p.ID {
				continue
			}
			name := p.Name
			seat := i
			wasInGame := r.InGame && r.State != nil

			// 对局中断线：保留座位，仅标记离线，避免牌局座位错乱
			if wasInGame && reason == "disconnect" {
				x.Online = false
				x.Conn = nil
				broadcast(r, "player_left", map[string]any{
					"player_id": p.ID,
					"name":      name,
					"seat":      seat,
					"reason":    "disconnect",
					"in_game":   true,
					"offline":   true,
					"owner":     r.Owner,
				})
				broadcast(r, "tips", map[string]any{
					"msg": name + " 断线了，等待重连",
				})
				// 若正好轮到他出牌，跳过以免整桌卡住
				if r.State != nil && r.State.Phase == "discard" && r.State.Current == seat {
					nextTurn(r, seat)
				}
				return
			}

			// 大厅离开 / 主动 leave：移除玩家
			r.Players = append(r.Players[:i], r.Players[i+1:]...)
			if len(r.Players) == 0 {
				delete(hub.rooms, id)
				return
			}
			if r.Owner == p.ID {
				r.Owner = r.Players[0].ID
			}
			broadcast(r, "player_left", map[string]any{
				"player_id": p.ID,
				"name":      name,
				"seat":      seat,
				"reason":    reason,
				"in_game":   wasInGame,
				"offline":   false,
				"owner":     r.Owner,
			})
			broadcast(r, "room", roomView(r))
			if wasInGame {
				broadcast(r, "tips", map[string]any{"msg": name + " 已离开房间"})
			}
			return
		}
	}
}

func leaveRoom(p *Player) {
	hub.mu.Lock()
	defer hub.mu.Unlock()
	removeFromRoomsLocked(p, "leave")
}

func onLeave(p *Player) {
	hub.mu.Lock()
	p.Online = false
	removeFromRoomsLocked(p, "disconnect")
	hub.mu.Unlock()
	if p.Conn != nil {
		_ = p.Conn.Close()
		p.Conn = nil
	}
}
func listRooms(p *Player) {
	hub.mu.Lock()
	defer hub.mu.Unlock()
	type item struct {
		ID     string `json:"id"`
		Rule   string `json:"rule"`
		Count  int    `json:"count"`
		InGame bool   `json:"in_game"`
	}
	list := make([]item, 0)
	for _, r := range hub.rooms {
		list = append(list, item{ID: r.ID, Rule: r.Rule, Count: len(r.Players), InGame: r.InGame})
	}
	reply(p.Conn, "room_list", list)
}

func startGame(p *Player) {
	hub.mu.Lock()
	defer hub.mu.Unlock()
	r := findRoom(p)
	if r == nil || r.Owner != p.ID {
		return
	}
	if len(r.Players) < 2 {
		reply(p.Conn, "error", map[string]string{"msg": "至少 2 人"})
		return
	}
	n := len(r.Players)
	var deck []game.Tile
	if r.Rule == "sichuan" {
		deck = game.SichuanDeck()
	} else {
		deck = game.FullDeck()
	}
	game.Shuffle(deck)
	st := &State{
		Rule: r.Rule, Hands: make([][]game.Tile, n), Discards: make([][]game.Tile, n),
		Melds: make([][]game.Meld, n), Won: make([]bool, n),
		Exchange: make([][]game.Tile, n), Exchanged: make([]bool, n),
		Que: make([]int, n), QueDone: make([]bool, n),
	}
	for i := 0; i < n; i++ {
		st.Que[i] = -1
	}
	for i := 0; i < n; i++ {
		st.Hands[i] = append([]game.Tile{}, deck[:13]...)
		deck = deck[13:]
		game.SortHand(st.Hands[i])
	}
	st.Wall = deck
	r.State = st
	r.InGame = true
	if r.Rule == "sichuan" {
		st.Phase = "exchange"
		for i, pl := range r.Players {
			reply(pl.Conn, "game_start", map[string]any{
				"seat": i, "rule": r.Rule, "hand": st.Hands[i], "exchange": true, "players": names(r),
			})
		}
		return
	}
	st.Current = 0
	st.Phase = "discard"
	drawOne(st, 0)
	for i, pl := range r.Players {
		reply(pl.Conn, "game_start", map[string]any{
			"seat": i, "rule": r.Rule, "hand": st.Hands[i], "exchange": false, "players": names(r),
		})
	}
	broadcast(r, "turn", map[string]any{"current": st.Current, "phase": st.Phase, "wall": len(st.Wall)})
}

func doExchange(p *Player, raw json.RawMessage) {
	var wrap struct {
		Tiles json.RawMessage `json:"tiles"`
	}
	if json.Unmarshal(raw, &wrap) != nil {
		reply(p.Conn, "error", map[string]string{"msg": "数据格式错误"})
		return
	}
	tiles, err := game.ParseTiles(wrap.Tiles)
	if err != nil || len(tiles) != 3 {
		reply(p.Conn, "error", map[string]string{"msg": "必须选 3 张有效牌"})
		return
	}
	suit := tiles[0].Suit
	for _, t := range tiles {
		if t.Suit != suit || t.Suit > game.Tiao {
			reply(p.Conn, "error", map[string]string{"msg": "换三张须同花色"})
			return
		}
	}
	hub.mu.Lock()
	defer hub.mu.Unlock()
	r := findRoom(p)
	if r == nil || r.State == nil || r.State.Phase != "exchange" {
		return
	}
	st := r.State
	seat := seatOf(r, p)
	if seat < 0 || st.Exchanged[seat] {
		return
	}
	hand := append([]game.Tile{}, st.Hands[seat]...)
	for _, t := range tiles {
		var ok bool
		hand, ok = game.RemoveOne(hand, t)
		if !ok {
			reply(p.Conn, "error", map[string]string{"msg": "手牌不足"})
			return
		}
	}
	st.Hands[seat] = hand
	st.Exchange[seat] = tiles
	st.Exchanged[seat] = true
	reply(p.Conn, "exchange_ok", map[string]any{"ok": true})
	for i := 0; i < len(r.Players); i++ {
		if !st.Exchanged[i] {
			return
		}
	}
	dice := rand.Intn(6) + rand.Intn(6) + 2
	dir := game.ExchangeDir(dice)
	n := len(r.Players)
	newHands := make([][]game.Tile, n)
	for i := 0; i < n; i++ {
		from := (i - dir + n*3) % n
		newHands[i] = append(st.Hands[i], st.Exchange[from]...)
		game.SortHand(newHands[i])
	}
	st.Hands = newHands
	// 记录每人新换入的 3 张供客户端高亮
	received := make([][]game.Tile, n)
	for i := 0; i < n; i++ {
		from := (i - dir + n*3) % n
		received[i] = append([]game.Tile{}, st.Exchange[from]...)
	}
	st.Phase = "dingque"
	for i, pl := range r.Players {
		reply(pl.Conn, "exchange_done", map[string]any{
			"hand": st.Hands[i], "direction": dir, "dice": dice,
			"received": received[i], "need_dingque": true,
		})
	}
}

func doDingQue(p *Player, raw json.RawMessage) {
	var d struct {
		Suit int `json:"suit"` // 0万 1筒 2条
	}
	if json.Unmarshal(raw, &d) != nil || d.Suit < 0 || d.Suit > 2 {
		reply(p.Conn, "error", map[string]string{"msg": "定缺须选万/筒/条"})
		return
	}
	hub.mu.Lock()
	defer hub.mu.Unlock()
	r := findRoom(p)
	if r == nil || r.State == nil || r.State.Phase != "dingque" {
		return
	}
	st := r.State
	seat := seatOf(r, p)
	if seat < 0 || st.QueDone[seat] {
		return
	}
	st.Que[seat] = d.Suit
	st.QueDone[seat] = true
	reply(p.Conn, "dingque_ok", map[string]any{"suit": d.Suit})
	broadcast(r, "dingque_update", map[string]any{"seat": seat, "suit": d.Suit})

	for i := 0; i < len(r.Players); i++ {
		if !st.QueDone[i] {
			return
		}
	}
	// 全部定缺完毕，庄家摸牌开始
	st.Phase = "discard"
	st.Current = 0
	drawOne(st, 0)
	ques := make([]int, len(r.Players))
	copy(ques, st.Que)
	for i, pl := range r.Players {
		reply(pl.Conn, "game_ready", map[string]any{
			"hand": st.Hands[i], "que": st.Que[i], "all_que": ques,
			"draw": st.Hands[i][len(st.Hands[i])-1],
		})
	}
	broadcast(r, "turn", map[string]any{"current": st.Current, "phase": st.Phase, "wall": len(st.Wall)})
}

func doDiscard(p *Player, raw json.RawMessage) {
	var wrap struct {
		Tile json.RawMessage `json:"tile"`
	}
	if json.Unmarshal(raw, &wrap) != nil {
		return
	}
	tile, err := game.ParseTile(wrap.Tile)
	if err != nil {
		reply(p.Conn, "error", map[string]string{"msg": "无效的牌"})
		return
	}
	hub.mu.Lock()
	defer hub.mu.Unlock()
	r := findRoom(p)
	if r == nil || r.State == nil {
		return
	}
	st := r.State
	seat := seatOf(r, p)
	if seat < 0 || seat != st.Current || st.Phase != "discard" || st.Won[seat] {
		reply(p.Conn, "error", map[string]string{"msg": "还没轮到你出牌"})
		return
	}
	// 川麻：手中还有定缺花色时，必须先打定缺
	if st.Rule == "sichuan" && st.Que[seat] >= 0 {
		hasQue := false
		for _, h := range st.Hands[seat] {
			if int(h.Suit) == st.Que[seat] {
				hasQue = true
				break
			}
		}
		if hasQue && int(tile.Suit) != st.Que[seat] {
			reply(p.Conn, "error", map[string]string{"msg": "须先打出定缺花色的牌"})
			return
		}
	}
	hand, ok := game.RemoveOne(st.Hands[seat], tile)
	if !ok {
		reply(p.Conn, "error", map[string]string{"msg": "手牌中没有这张牌"})
		return
	}
	st.Hands[seat] = hand
	game.SortHand(st.Hands[seat])
	st.Discards[seat] = append(st.Discards[seat], tile)
	st.LastFrom = seat
	st.LastTile = tile
	st.Phase = "wait_action"
	st.Passed = make([]bool, len(r.Players))
	st.NeedAct = make([]bool, len(r.Players))
	broadcast(r, "discarded", map[string]any{"seat": seat, "tile": tile})
	reply(p.Conn, "hand", map[string]any{"hand": st.Hands[seat]})
	need := false
	for i := 0; i < len(r.Players); i++ {
		if i == seat || st.Won[i] {
			continue
		}
		cand := append(append([]game.Tile{}, st.Hands[i]...), tile)
		canP := game.CanPung(st.Hands[i], tile)
		// 推倒胡只允许自摸；四川可点炮且必须已定缺、手牌无定缺花色
		canW := false
		if st.Rule == "sichuan" && st.Que[i] >= 0 {
			canW = game.CanWinSichuan(cand, st.Melds[i], game.Suit(st.Que[i]))
		}
		// 四川：不能碰定缺花色
		if st.Rule == "sichuan" && st.Que[i] >= 0 && int(tile.Suit) == st.Que[i] {
			canP = false
		}
		if canP || canW {
			need = true
			st.NeedAct[i] = true
			reply(r.Players[i].Conn, "action_prompt", map[string]any{
				"tile": tile, "from": seat, "can_pung": canP, "can_win": canW, "self": false,
			})
		}
	}
	if !need {
		nextTurn(r, seat)
	}
}

func doAction(p *Player, raw json.RawMessage) {
	var d struct {
		Action string `json:"action"`
	}
	json.Unmarshal(raw, &d)
	hub.mu.Lock()
	defer hub.mu.Unlock()
	r := findRoom(p)
	if r == nil || r.State == nil || r.State.Phase != "wait_action" {
		return
	}
	st := r.State
	seat := seatOf(r, p)
	if seat < 0 || st.Won[seat] || !st.NeedAct[seat] {
		return
	}
	switch d.Action {
	case "pung":
		if !game.CanPung(st.Hands[seat], st.LastTile) {
			return
		}
		hand, ok := game.RemoveN(st.Hands[seat], st.LastTile, 2)
		if !ok {
			return
		}
		st.Hands[seat] = hand
		t := st.LastTile
		st.Melds[seat] = append(st.Melds[seat], game.Meld{Type: game.MeldPung, Tiles: []game.Tile{t, t, t}})
		st.Current = seat
		st.Phase = "discard"
		if len(st.Discards[st.LastFrom]) > 0 {
			st.Discards[st.LastFrom] = st.Discards[st.LastFrom][:len(st.Discards[st.LastFrom])-1]
		}
		broadcast(r, "pung", map[string]any{"seat": seat, "tile": t, "from": st.LastFrom})
		reply(p.Conn, "hand", map[string]any{"hand": st.Hands[seat]})
		broadcast(r, "turn", map[string]any{"current": st.Current, "phase": st.Phase, "wall": len(st.Wall)})
	case "win":
		// 推倒胡禁止点炮，只能 self_win
		if st.Rule != "sichuan" {
			reply(p.Conn, "error", map[string]string{"msg": "推倒胡只能自摸"})
			return
		}
		if st.Que[seat] < 0 {
			reply(p.Conn, "error", map[string]string{"msg": "请先定缺"})
			return
		}
		cand := append(append([]game.Tile{}, st.Hands[seat]...), st.LastTile)
		if !game.CanWinSichuan(cand, st.Melds[seat], game.Suit(st.Que[seat])) {
			reply(p.Conn, "error", map[string]string{"msg": "未定缺干净或牌型不能胡"})
			return
		}
		st.Hands[seat] = cand
		st.Won[seat] = true
		cont := st.Rule == "sichuan" && !sichuanShouldEnd(st, len(r.Players))
		broadcast(r, "won", map[string]any{
			"seat": seat, "from": st.LastFrom, "hand": st.Hands[seat],
			"continue": cont, "won_seats": wonSeatList(st),
		})
		if st.Rule == "sichuan" {
			afterSichuanWin(r, st.LastFrom)
		} else {
			endGame(r)
		}
	case "pass":
		st.Passed[seat] = true
		st.NeedAct[seat] = false
		for i := range st.NeedAct {
			if st.NeedAct[i] && !st.Passed[i] {
				return
			}
		}
		nextTurn(r, st.LastFrom)
	}
}

func doSelfWin(p *Player) {
	hub.mu.Lock()
	defer hub.mu.Unlock()
	r := findRoom(p)
	if r == nil || r.State == nil {
		return
	}
	st := r.State
	seat := seatOf(r, p)
	if seat < 0 || seat != st.Current || st.Phase != "discard" || st.Won[seat] {
		return
	}
	if st.Rule == "sichuan" {
		if st.Que[seat] < 0 {
			reply(p.Conn, "error", map[string]string{"msg": "请先定缺"})
			return
		}
		if !game.CanWinSichuan(st.Hands[seat], st.Melds[seat], game.Suit(st.Que[seat])) {
			reply(p.Conn, "error", map[string]string{"msg": "未定缺干净或牌型不能胡"})
			return
		}
	} else if !game.CanWin(st.Hands[seat]) {
		return
	}
	st.Won[seat] = true
	cont := st.Rule == "sichuan" && !sichuanShouldEnd(st, len(r.Players))
	broadcast(r, "won", map[string]any{
		"seat": seat, "from": -1, "hand": st.Hands[seat],
		"continue": cont, "won_seats": wonSeatList(st),
	})
	if st.Rule == "sichuan" {
		afterSichuanWin(r, seat)
	} else {
		endGame(r)
	}
}

func nextTurn(r *Room, after int) {
	st := r.State
	n := len(r.Players)
	for k := 1; k <= n; k++ {
		i := (after + k) % n
		if st.Won[i] {
			continue
		}
		// 离线座位跳过（等待其重连期间不卡死整桌）
		if i < len(r.Players) && r.Players[i] != nil && !r.Players[i].Online {
			continue
		}
		if len(st.Wall) == 0 {
			endGame(r)
			return
		}
		drawOne(st, i)
		st.Current = i
		st.Phase = "discard"
		canSelf := false
		if st.Rule == "sichuan" {
			if st.Que[i] >= 0 {
				canSelf = game.CanWinSichuan(st.Hands[i], st.Melds[i], game.Suit(st.Que[i]))
			}
		} else {
			canSelf = game.CanWin(st.Hands[i])
		}
		if canSelf {
			reply(r.Players[i].Conn, "action_prompt", map[string]any{
				"tile": st.Hands[i][len(st.Hands[i])-1], "from": -1, "can_pung": false, "can_win": true, "self": true,
			})
		}
		reply(r.Players[i].Conn, "draw", map[string]any{"tile": st.Hands[i][len(st.Hands[i])-1], "hand": st.Hands[i]})
		broadcast(r, "turn", map[string]any{"current": st.Current, "phase": st.Phase, "wall": len(st.Wall)})
		return
	}
	endGame(r)
}


// sichuanShouldEnd 血战：未胡人数 <=1，或已有3人胡（4人标准）
func wonSeatList(st *State) []int {
	out := make([]int, 0)
	for i, w := range st.Won {
		if w {
			out = append(out, i)
		}
	}
	return out
}

func sichuanShouldEnd(st *State, n int) bool {
	wonCount := 0
	for _, w := range st.Won {
		if w {
			wonCount++
		}
	}
	alive := n - wonCount
	if alive <= 1 {
		return true
	}
	// 4 人局经典：3 人胡完结束
	if n >= 4 && wonCount >= 3 {
		return true
	}
	return false
}

func afterSichuanWin(r *Room, afterSeat int) {
	st := r.State
	n := len(r.Players)
	// 清理等待操作状态
	st.Phase = "discard"
	st.NeedAct = make([]bool, n)
	st.Passed = make([]bool, n)
	if sichuanShouldEnd(st, n) {
		endGame(r)
		return
	}
	nextTurn(r, afterSeat)
}
func drawOne(st *State, seat int) {
	if len(st.Wall) == 0 {
		return
	}
	st.Hands[seat] = append(st.Hands[seat], st.Wall[0])
	st.Wall = st.Wall[1:]
}

func endGame(r *Room) {
	st := r.State
	type row struct {
		Name string      `json:"name"`
		Won  bool        `json:"won"`
		Hand []game.Tile `json:"hand"`
	}
	var res []row
	for i, pl := range r.Players {
		res = append(res, row{Name: pl.Name, Won: st.Won[i], Hand: st.Hands[i]})
	}
	broadcast(r, "game_over", map[string]any{"result": res})
	r.InGame = false
	r.State = nil
	broadcast(r, "room", roomView(r))
}

func findRoom(p *Player) *Room {
	for _, r := range hub.rooms {
		for _, x := range r.Players {
			if x.ID == p.ID {
				return r
			}
		}
	}
	return nil
}

func seatOf(r *Room, p *Player) int {
	for i, x := range r.Players {
		if x.ID == p.ID {
			return i
		}
	}
	return -1
}

func names(r *Room) []string {
	out := make([]string, len(r.Players))
	for i, p := range r.Players {
		out[i] = p.Name
	}
	return out
}

func roomView(r *Room) map[string]any {
	ps := make([]map[string]any, 0, len(r.Players))
	for _, p := range r.Players {
		ps = append(ps, map[string]any{"id": p.ID, "name": p.Name, "online": p.Online, "gold": p.Gold})
	}
	return map[string]any{"id": r.ID, "rule": r.Rule, "owner": r.Owner, "in_game": r.InGame, "players": ps}
}

func reply(c *websocket.Conn, typ string, data any) {
	if c == nil {
		return
	}
	b, _ := json.Marshal(data)
	_ = c.WriteJSON(Msg{Type: typ, Data: b})
}

func broadcast(r *Room, typ string, data any) {
	for _, p := range r.Players {
		if p != nil && p.Online && p.Conn != nil {
			reply(p.Conn, typ, data)
		}
	}
}

func id6() string {
	const c = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	b := make([]byte, 6)
	for i := range b {
		b[i] = c[rand.Intn(len(c))]
	}
	return string(b)
}

func id4() string { return id6()[:4] }
