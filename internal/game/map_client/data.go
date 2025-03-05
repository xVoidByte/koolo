package map_client

type ServerLevel struct {
	Type   string         `json:"type"`
	ID     int            `json:"id"`
	Name   string         `json:"name"`
	Offset ServerPosition `json:"offset"`
	Size   struct {
		Width  int `json:"width"`
		Height int `json:"height"`
	} `json:"size"`
	Objects []ServerObject `json:"objects"`
	Rooms   []ServerRoom   `json:"rooms"`
	Map     [][]int
}

type ServerPosition struct {
	X int `json:"x"`
	Y int `json:"y"`
}

type ServerObject struct {
	ID   int    `json:"id"`
	Type string `json:"type"`
	Name string `json:"name"`
	ServerPosition
}

type ServerRoom struct {
	ServerPosition
	Width  int `json:"width"`
	Height int `json:"height"`
}
