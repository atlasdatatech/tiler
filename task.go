package main

import (
	"bytes"
	"compress/gzip"
	"database/sql"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/paulmach/orb"
	"github.com/paulmach/orb/clip"
	"github.com/paulmach/orb/geojson"
	"github.com/paulmach/orb/maptile"
	"github.com/paulmach/orb/maptile/tilecover"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"github.com/teris-io/shortid"
	pb "gopkg.in/cheggaaa/pb.v1"
)

//MBTileVersion mbtiles版本号
const MBTileVersion = "1.2"

//Task 下载任务
type Task struct {
	ID                 string
	Name               string
	Description        string
	File               string
	Min                int
	Max                int
	Layers             []Layer
	TileMap            TileMap
	Total              int64
	Current            int64
	Bar                *pb.ProgressBar
	db                 *sql.DB
	workerCount        int
	savePipeSize       int
	bufSize            int
	wg                 sync.WaitGroup
	abort, pause, play chan struct{}
	workers            chan maptile.Tile
	savingpipe         chan Tile
	tileSet            Set
	outformat          string
}

//NewTask 创建下载任务
func NewTask(layers []Layer, m TileMap) *Task {
	if len(layers) == 0 {
		return nil
	}
	id, _ := shortid.Generate()

	task := Task{
		ID:      id,
		Name:    m.Name,
		Layers:  layers,
		Min:     layers[0].Zoom,
		Max:     layers[len(layers)-1].Zoom,
		TileMap: m,
	}

	for i := 0; i < len(layers); i++ {
		if layers[i].URL == "" {
			layers[i].URL = m.URL
		}
		t := time.Now()
		layers[i].Count = tilecover.CollectionCount(layers[i].Collection, maptile.Zoom(layers[i].Zoom))
		fmt.Println(time.Since(t))
		fmt.Println(layers[i].Zoom, layers[i].Count)
		task.Total += layers[i].Count
	}
	task.abort = make(chan struct{})
	task.pause = make(chan struct{})
	task.play = make(chan struct{})

	task.workerCount = viper.GetInt("task.workers")
	task.savePipeSize = viper.GetInt("task.savepipe")
	task.workers = make(chan maptile.Tile, task.workerCount)
	task.savingpipe = make(chan Tile, task.savePipeSize)
	task.bufSize = viper.GetInt("task.mergebuf")
	task.tileSet = Set{M: make(maptile.Set)}

	task.outformat = viper.GetString("output.format")
	return &task
}

//Bound 范围
func (task *Task) Bound() orb.Bound {
	bound := orb.Bound{}
	for _, layer := range task.Layers {
		for _, g := range layer.Collection {
			bound = bound.Union(g.Bound())
		}
	}
	return bound
}

//Center 中心点
func (task *Task) Center() orb.Point {
	layer := task.Layers[len(task.Layers)-1]
	bound := orb.Bound{}
	for _, g := range layer.Collection {
		bound = bound.Union(g.Bound())
	}
	return bound.Center()
}

//MetaItems 输出
func (task *Task) MetaItems() map[string]string {
	b := task.Bound()
	c := task.Center()
	data := map[string]string{
		"id":          task.ID,
		"name":        task.Name,
		"description": task.Description,
		"attribution": `<a href="http://www.atlasdata.cn/" target="_blank">&copy; MapCloud</a>`,
		"basename":    task.TileMap.Name,
		"format":      task.TileMap.Format,
		"type":        task.TileMap.Schema,
		"pixel_scale": strconv.Itoa(TileSize),
		"version":     MBTileVersion,
		"bounds":      fmt.Sprintf(`%f,%f,%f,%f`, b.Left(), b.Bottom(), b.Right(), b.Top()),
		"center":      fmt.Sprintf(`%f,%f,%d`, c.X(), c.Y(), (task.Min+task.Max)/2),
		"minzoom":     strconv.Itoa(task.Min),
		"maxzoom":     strconv.Itoa(task.Max),
		"json": `
		{"vector_layers": [{
			"description": "",
			"fields": {
				"abstract_color": "abstract_color",
				"namec": "namec",
				"names": "names",
				"rail_type": "rail_type"
			},
			"id": "cn_railway_label"
		}, {
			"description": "",
			"fields": {
				"abstract_color": "abstract_color",
				"rail_type": "rail_type"
			},
			"id": "cn_railway"
		}, {
			"description": "",
			"fields": {
				"door": "door",
				"featgroup": "featgroup",
				"feattype": "feattype",
				"maki": "maki",
				"namep": "name_pny",
				"namec": "name_zh",
				"names": "name_zh_short",
				"network": "network"
			},
			"id": "cn_railstation_label"
		}, {
			"description": "",
			"fields": {},
			"id": "cn_ocean"
		}, {
			"description": "",
			"fields": {
				"dir": "dir",
				"namep": "name_pny",
				"namec": "name_zh",
				"names": "name_zh_short",
				"type": "type"
			},
			"id": "cn_place_label"
		}, {
			"description": "",
			"fields": {},
			"id": "cn_water_polygon_lz"
		}, {
			"description": "",
			"fields": {},
			"id": "cn_riverlines"
		}, {
			"description": "",
			"fields": {
				"area": "area",
				"dir": "dir",
				"namec": "name_zh"
			},
			"id": "cn_island_label"
		}, {
			"description": "",
			"fields": {
				"featcode": "featcode",
				"floor": "floor"
			},
			"id": "cn_building"
		}, {
			"description": "",
			"fields": {
				"featcode": "featcode",
				"name_zh": "name_zh",
				"scalerank": "scalerank"
			},
			"id": "cn_water_line_label"
		}, {
			"description": "",
			"fields": {
				"area": "area",
				"featcode": "featcode",
				"namec": "name_zh",
				"scalerank": "scalerank"
			},
			"id": "cn_water_point_label"
		}, {
			"description": "",
			"fields": {
				"fc": "func_class",
				"vl": "layer",
				"namep": "name_pny",
				"namec": "name_zh",
				"oneway": "oneway",
				"ref": "ref",
				"reflen": "reflen",
				"fw": "road_char",
				"shield": "shield",
				"structure": "structure",
				"type": "type"
			},
			"id": "cn_road_label"
		}, {
			"description": "",
			"fields": {
				"fc": "func_class",
				"vl": "layer",
				"oneway": "oneway",
				"fw": "road_char",
				"structure": "structure",
				"type": "type"
			},
			"id": "cn_road"
		}, {
			"description": "",
			"fields": {},
			"id": "cn_river"
		}, {
			"description": "",
			"fields": {
				"class": "class",
				"scalerank": "scalerank"
			},
			"id": "cn_park"
		}, {
			"description": "",
			"fields": {
				"admin_level": "Number",
				"disputed": "Number"
			},
			"id": "cn_boundary"
		}, {
			"description": "",
			"fields": {
				"door": "door",
				"featgroup": "featgroup",
				"feattype": "feattype",
				"maki": "maki",
				"namep": "name_pny",
				"namec": "name_zh",
				"names": "name_zh_short",
				"network": "network",
				"scalerank": "scalerank"
			},
			"id": "cn_poi_label"
		}, {
			"description": "",
			"fields": {
				"feattype": "feattype",
				"namep": "name_pny",
				"namec": "name_zh"
			},
			"id": "cn_junction"
		}, {
			"description": "",
			"fields": {
				"class": "class"
			},
			"id": "cn_landuse"
		}, {
			"description": "",
			"fields": {},
			"id": "cn_island"
		}, {
			"description": "",
			"fields": {
				"disputed": "disputed",
				"admin_level": "admin_level",
				"maritime": "maritime",
				"iso_3166_1": "iso_3166_1"
			},
			"id": "cn_boundary_global"
		}, {
			"description": "",
			"fields": {
				"name_es": "name_es",
				"name_zh": "name_zh",
				"parent": "parent",
				"name_fr": "name_fr",
				"code": "code",
				"name_zh-CN": "name_zh-CN",
				"scalerank": "scalerank",
				"type": "type",
				"name_ru": "name_ru",
				"name_de": "name_de",
				"name": "name",
				"name_en": "name_en"
			},
			"id": "cn_country_label"
		}, {
			"description": "",
			"fields": {
				"localrank": "localrank",
				"name_zh": "name_zh",
				"capital": "capital",
				"ldir": "ldir",
				"scalerank": "scalerank",
				"type": "type"
			},
			"id": "cn_place_global"
		}, {
			"description": "",
			"fields": {
				"class": "One of: agriculture, cemetery, glacier, grass, hospital, industrial, park, parking, piste, pitch, rock, sand, school, scrub, wood, aboriginal lands",
				"type": "OSM tag, more specific than class"
			},
			"id": "landuse"
		}, {
			"description": "",
			"fields": {
				"class": "One of: river, canal, stream, stream_intermittent, ditch, drain",
				"type": "One of: river, canal, stream, ditch, drain"
			},
			"id": "waterway"
		}, {
			"description": "",
			"fields": {},
			"id": "water"
		}, {
			"description": "",
			"fields": {
				"type": "One of: runway, taxiway, apron"
			},
			"id": "aeroway"
		}, {
			"description": "",
			"fields": {
				"class": "One of: fence, hedge, cliff, gate"
			},
			"id": "barrier_line"
		}, {
			"description": "",
			"fields": {
				"underground": "Text. Whether building is underground. One of: 'true', 'false'"
			},
			"id": "building"
		}, {
			"description": "",
			"fields": {
				"class": "One of: national_park, wetland, wetland_noveg",
				"type": "OSM tag, more specific than class"
			},
			"id": "landuse_overlay"
		}, {
			"description": "",
			"fields": {
				"class": "One of: 'motorway', 'motorway_link', 'trunk', 'primary', 'secondary', 'tertiary', 'link', 'street', 'street_limited', 'pedestrian', 'construction', 'track', 'service', 'ferry', 'path', 'golf'",
				"layer": "Number. Specifies z-ordering in the case of overlapping road segments. Common range is -5 to 5. Available from zoom level 13+.",
				"oneway": "Text. Whether traffic on the road is one-way. One of: 'true', 'false'",
				"structure": "Text. One of: 'none', 'bridge', 'tunnel', 'ford'. Available from zoom level 13+.",
				"type": "In most cases, values will be that of the primary key from OpenStreetMap tags."
			},
			"id": "road"
		}, {
			"description": "",
			"fields": {
				"admin_level": "The OSM administrative level of the boundary",
				"disputed": "Number. Disputed boundaries are 1, all others are 0.",
				"iso_3166_1": "The ISO 3166-1 alpha-2 code(s) of the state(s) a boundary is part of. Format: 'AA' or 'AA-BB'",
				"maritime": "Number. Maritime boundaries are 1, all others are 0."
			},
			"id": "admin"
		}, {
			"description": "",
			"fields": {
				"code": "ISO 3166-1 Alpha-2 code",
				"name": "Local name of the country",
				"name_de": "German name of the country",
				"name_en": "English name of the country",
				"name_es": "Spanish name of the country",
				"name_fr": "French name of the country",
				"name_ru": "Russian name of the country",
				"name_zh": "Chinese name of the country",
				"parent": "ISO 3166-1 Alpha-2 code of the administering/parent state, if any",
				"scalerank": "Number, 1-6. Useful for styling text sizes.",
				"type": "One of: country, territory, disputed territory, sar"
			},
			"id": "country_label"
		}, {
			"description": "",
			"fields": {
				"labelrank": "Number, 1-6. Useful for styling text sizes.",
				"name": "Local or international name of the water body",
				"name_de": "German name of the water body",
				"name_en": "English name of the water body",
				"name_es": "Spanish name of the water body",
				"name_fr": "French name of the water body",
				"name_ru": "Russian name of the water body",
				"name_zh": "Chinese name of the water body",
				"placement": "One of: point, line"
			},
			"id": "marine_label"
		}, {
			"description": "",
			"fields": {
				"abbr": "Abbreviated state name",
				"area": "The area of the state in kilometers²",
				"name": "Local name of the state",
				"name_de": "German name of the state",
				"name_en": "English name of the state",
				"name_es": "Spanish name of the state",
				"name_fr": "French name of the state",
				"name_ru": "Russian name of the state",
				"name_zh": "Chinese name of the state"
			},
			"id": "state_label"
		}, {
			"description": "",
			"fields": {
				"capital": "Admin level the city is a capital of, if any. One of: 2, 3, 4, 5, 6, null",
				"ldir": "A hint for label placement at low zoom levels. One of: N, E, S, W, NE, SE, SW, NW, null",
				"localrank": "Number. Priority relative to nearby places. Useful for limiting label density.",
				"name": "Local name of the place",
				"name_de": "German name of the place",
				"name_en": "English name of the place",
				"name_es": "Spanish name of the place",
				"name_fr": "French name of the place",
				"name_ru": "Russian name of the place",
				"name_zh": "Chinese name of the place",
				"scalerank": "Number, 0-9 or null. Useful for styling text & marker sizes.",
				"type": "One of: city, town, village, hamlet, suburb, neighbourhood, island, islet, archipelago, residential, aboriginal_lands"
			},
			"id": "place_label"
		}, {
			"description": "",
			"fields": {
				"area": "The area of the water polygon in Mercator meters²",
				"name": "Local name of the water body",
				"name_de": "German name of the water body",
				"name_en": "English name of the water body",
				"name_es": "Spanish name of the water body",
				"name_fr": "French name of the water body",
				"name_ru": "Russian name of the water body",
				"name_zh": "Chinese name of the water body"
			},
			"id": "water_label"
		}, {
			"description": "",
			"fields": {
				"maki": "One of: airport, airfield, heliport, rocket",
				"name": "Local name of the airport",
				"name_de": "German name of the airport",
				"name_en": "English name of the airport",
				"name_es": "Spanish name of the airport",
				"name_fr": "French name of the airport",
				"name_ru": "Russian name of the airport",
				"name_zh": "Chinese name of the airport",
				"ref": "A 3-4 character IATA, FAA, ICAO, or other reference code",
				"scalerank": "Number 1-4. Useful for styling icon sizes."
			},
			"id": "airport_label"
		}, {
			"description": "",
			"fields": {
				"maki": "One of: rail, rail-metro, rail-light, entrance",
				"name": "Local name of the station",
				"name_de": "German name of the station",
				"name_en": "English name of the station",
				"name_es": "Spanish name of the station",
				"name_fr": "French name of the station",
				"name_ru": "Russian name of the station",
				"name_zh": "Chinese name of the station",
				"network": "The network(s) that the station serves. Useful for icon styling."
			},
			"id": "rail_station_label"
		}, {
			"description": "",
			"fields": {
				"elevation_ft": "Integer elevation in feet",
				"elevation_m": "Integer elevation in meters",
				"maki": "One of: 'mountain', 'volcano'",
				"name": "Local name of the peak",
				"name_de": "German name of the peak",
				"name_en": "English name of the peak",
				"name_es": "Spanish name of the peak",
				"name_fr": "French name of the peak",
				"name_ru": "Russian name of the peak",
				"name_zh": "Chinese name of the peak"
			},
			"id": "mountain_peak_label"
		}, {
			"description": "",
			"fields": {
				"localrank": "Number. Priority relative to nearby POIs. Useful for limiting label density.",
				"maki": "The name of the Maki icon that should be used for the POI",
				"name": "Local name of the POI",
				"name_de": "German name of the POI",
				"name_en": "English name of the POI",
				"name_es": "Spanish name of the POI",
				"name_fr": "French name of the POI",
				"name_ru": "Russian name of the POI",
				"name_zh": "Chinese name of the POI",
				"ref": "Short reference code, if any",
				"scalerank": "Number. 1-5. Useful for styling icon sizes and minimum zoom levels.",
				"type": "The original OSM tag value"
			},
			"id": "poi_label"
		}, {
			"description": "",
			"fields": {
				"class": "The class of road the junction is on. Matches the classes in the road layer.",
				"name": "A longer name",
				"ref": "A short identifier",
				"reflen": "The number of characters in the ref field.",
				"type": "The type of road the junction is on. Matches the types in the road layer."
			},
			"id": "motorway_junction"
		}, {
			"description": "",
			"fields": {
				"class": "One of: motorway, motorway_link, 'trunk', 'primary', 'secondary', 'tertiary', 'link', 'street', 'street_limited', 'pedestrian', 'construction', 'track', 'service', 'ferry', 'path', 'golf'",
				"iso_3166_2": "Text. The ISO 3166-2 code of the state/province/region the road is in.",
				"len": "Number. Approximate length of the road segment in Mercator meters.",
				"localrank": "Number. Used for shield points only. Priority relative to nearby shields. Useful for limiting shield density.",
				"name": "Local name of the road",
				"name_de": "German name of the road",
				"name_en": "English name of the road",
				"name_es": "Spanish name of the road",
				"name_fr": "French name of the road",
				"name_ru": "Russian name of the road",
				"name_zh": "Chinese name of the road",
				"ref": "Route number of the road",
				"reflen": "Number. How many characters long the ref tag is. Useful for shield styling.",
				"shield": "The shield style to use. One of: default, mx-federal, mx-state, us-highway, us-highway-alternate, us-highway-business, us-highway-duplex, us-interstate, us-interstate-business, us-interstate-duplex, us-interstate-truck, us-state"
			},
			"id": "road_label"
		}, {
			"description": "",
			"fields": {
				"class": "One of: river, canal, stream, stream_intermittent",
				"name": "Local name of the waterway",
				"name_de": "German name of the waterway",
				"name_en": "English name of the waterway",
				"name_es": "Spanish name of the waterway",
				"name_fr": "French name of the waterway",
				"name_ru": "Russian name of the waterway",
				"name_zh": "Chinese name of the waterway",
				"type": "One of: river, canal, stream"
			},
			"id": "waterway_label"
		}, {
			"description": "",
			"fields": {
				"house_num": "House number"
			},
			"id": "housenum_label"
		}]}
		`,
	}
	return data
}

//SetupMBTileTables 初始化配置MBTile库
func (task *Task) SetupMBTileTables() error {

	if task.File == "" {
		outdir := viper.GetString("output.directory")
		os.MkdirAll(outdir, os.ModePerm)
		task.File = filepath.Join(outdir, task.ID+"."+task.TileMap.Name+".mbtiles")
	}
	os.Remove(task.File)
	db, err := sql.Open("sqlite3", task.File)
	if err != nil {
		return err
	}

	err = optimizeConnection(db)
	if err != nil {
		return err
	}

	_, err = db.Exec("create table if not exists tiles (zoom_level integer, tile_column integer, tile_row integer, tile_data blob);")
	if err != nil {
		return err
	}

	_, err = db.Exec("create table if not exists metadata (name text, value text);")
	if err != nil {
		return err
	}

	_, err = db.Exec("create unique index name on metadata (name);")
	if err != nil {
		return err
	}

	_, err = db.Exec("create unique index tile_index on tiles(zoom_level, tile_column, tile_row);")
	if err != nil {
		return err
	}

	// Load metadata.
	for name, value := range task.MetaItems() {
		_, err := db.Exec("insert into metadata (name, value) values (?, ?)", name, value)
		if err != nil {
			return err
		}
	}

	task.db = db //保存任务的库连接
	return nil
}

func (task *Task) abortFun() {
	// os.Stdin.Read(make([]byte, 1)) // read a single byte
	// <-time.After(8 * time.Second)
	task.abort <- struct{}{}
}

func (task *Task) pauseFun() {
	// os.Stdin.Read(make([]byte, 1)) // read a single byte
	// <-time.After(3 * time.Second)
	task.pause <- struct{}{}
}

func (task *Task) playFun() {
	// os.Stdin.Read(make([]byte, 1)) // read a single byte
	// <-time.After(5 * time.Second)
	task.play <- struct{}{}
}

//SavePipe 保存瓦片管道
func (task *Task) savePipe() {
	for tile := range task.savingpipe {
		err := saveToMBTile(tile, task.db)
		if err != nil {
			log.Errorf("save %v tile to mbtiles db error ~ %s", tile.T, err)
		}
	}
}

//SaveTile 保存瓦片
func (task *Task) saveTile(tile Tile) error {
	defer task.wg.Done()
	err := saveToFiles(tile, filepath.Base(task.File))
	if err != nil {
		log.Errorf("create %v tile file error ~ %s", tile.T, err)
	}
	return nil
}

//tileFetcher 瓦片加载器
func (task *Task) tileFetcher(t maptile.Tile, url string) {
	defer task.wg.Done()
	defer func() {
		<-task.workers
	}()
	start := time.Now()
	prep := func(t maptile.Tile, url string) string {
		url = strings.Replace(url, "{x}", strconv.Itoa(int(t.X)), -1)
		url = strings.Replace(url, "{y}", strconv.Itoa(int(t.Y)), -1)
		url = strings.Replace(url, "{z}", strconv.Itoa(int(t.Z)), -1)
		return url
	}
	pbf := prep(t, url)
	resp, err := http.Get(pbf)
	if err != nil {
		log.Errorf("fetch :%s error, details: %s ~", pbf, err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		log.Errorf("fetch %v tile error, status code: %d ~", pbf, resp.StatusCode)
		return
	}
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Errorf("read %v tile error ~ %s", t, err)
		return
	}
	if len(body) == 0 {
		log.Warnf("nil tile %v ~", t)
		return //zero byte tiles n
	}

	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_, err = zw.Write(body)
	if err != nil {
		log.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		log.Fatal(err)
	}
	tile := Tile{
		T: t,
		C: buf.Bytes(),
	}
	//enable savingpipe
	if task.outformat == "mbtiles" {
		task.savingpipe <- tile
	} else {
		task.wg.Add(1)
		task.saveTile(tile)
	}
	secs := time.Since(start).Seconds()
	fmt.Printf("\ntile %v, %.3fs, %.2f kb, %s ...\n", t, secs, float32(len(buf.Bytes()))/1024.0, pbf)
}

//DownloadZoom 下载指定层级
func (task *Task) downloadLayer(layer Layer) {
	bar := pb.New64(layer.Count).Prefix(fmt.Sprintf("Zoom %d : ", layer.Zoom)).Postfix("\n")
	// bar.SetRefreshRate(time.Second)
	bar.Start()
	// bar.SetMaxWidth(300)

	var tilelist = make(chan maptile.Tile, task.bufSize)

	go tilecover.CollectionChannel(layer.Collection, maptile.Zoom(layer.Zoom), tilelist)

	for tile := range tilelist {
		// log.Infof(`fetching tile %v ~`, tile)
		select {
		case task.workers <- tile:
			bar.Increment()
			task.Bar.Increment()
			task.wg.Add(1)
			go task.tileFetcher(tile, layer.URL)
		case <-task.abort:
			log.Infof("task %s got canceled.", task.ID)
			close(tilelist)
		case <-task.pause:
			log.Infof("task %s suspended.", task.ID)
			select {
			case <-task.play:
				log.Infof("task %s go on.", task.ID)
			case <-task.abort:
				log.Infof("task %s got canceled.", task.ID)
				close(tilelist)
			}
		}
	}
	task.wg.Wait()
	bar.FinishPrint(fmt.Sprintf("Task %s zoom %d finished ~", task.ID, layer.Zoom))
}

//DownloadZoom 下载指定层级
func (task *Task) downloadGeom(geom orb.Geometry, zoom int) {
	count := tilecover.GeometryCount(geom, maptile.Zoom(zoom))
	bar := pb.New64(count).Prefix(fmt.Sprintf("Zoom %d : ", zoom))
	bar.Start()

	var tilelist = make(chan maptile.Tile, task.bufSize)

	go func(g orb.Geometry, z maptile.Zoom, ch chan<- maptile.Tile) {
		defer close(ch)
		tilecover.GeometryChannel(g, z, ch)
	}(geom, maptile.Zoom(zoom), tilelist)

	for tile := range tilelist {
		// log.Infof(`fetching tile %v ~`, tile)
		select {
		case task.workers <- tile:
			bar.Increment()
			task.Bar.Increment()
			task.wg.Add(1)
			go task.tileFetcher(tile, task.TileMap.URL)
		case <-task.abort:
			log.Infof("task %s got canceled.", task.ID)
			close(tilelist)
		case <-task.pause:
			log.Infof("task %s suspended.", task.ID)
			select {
			case <-task.play:
				log.Infof("task %s go on.", task.ID)
			case <-task.abort:
				log.Infof("task %s got canceled.", task.ID)
				close(tilelist)
			}
		}
	}
	task.wg.Wait()
	bar.FinishPrint(fmt.Sprintf("task %s zoom %d finished ~", task.ID, zoom))
}

//Download 开启下载任务
func (task *Task) Download() {
	//g orb.Geometry, minz int, maxz int
	task.Bar = pb.New64(task.Total).Prefix("Task : ")
	//.Postfix("\n")
	// task.Bar.SetRefreshRate(10 * time.Second)
	// task.Bar.Format("<.- >")
	task.Bar.Start()
	if task.outformat == "mbtiles" {
		task.SetupMBTileTables()
	}
	go task.savePipe()
	for _, layer := range task.Layers {
		task.downloadLayer(layer)
	}
	task.wg.Wait()
	task.Bar.FinishPrint(fmt.Sprintf("task %s finished ~", task.ID))
}

//DownloadDepth 深度优先下载
func (task *Task) DownloadDepth() {
	task.Bar = pb.New64(task.Total).Prefix("Fetching -> ").Postfix("\n")
	task.Bar.Start()
	// for _, layer := range task.Layers {
	// 	task.downloadLayer(layer)
	// 	break
	// }
	for i, layer := range task.Layers {
		if i < 1 {
			continue
		}
		task.tileSet.Lock()
		zoomSet := task.tileSet.M
		mfc := task.tileSet.M.ToFeatureCollection()
		ifile := len(task.tileSet.M)
		fmt.Printf("merging up append set, %d tiles ~\n", ifile)
		fmt.Println("downloading started, Zoom:", layer.Zoom, "Tiles:", ifile)
		bar := pb.StartNew(ifile).Prefix(fmt.Sprintf("Zoom %d : ", layer.Zoom)).Postfix("\n")
		task.wg.Add(1)
		go func(name string, mfc *geojson.FeatureCollection) {
			defer task.wg.Done()
			data, err := json.MarshalIndent(mfc, "", " ")
			if err != nil {
				log.Printf("error marshalling json: %v", err)
				return
			}

			err = ioutil.WriteFile(name+".geojson", data, 0644)
			if err != nil {
				log.Printf("write file failure: %v", err)
			}
			log.Printf("output finished : %s.geojson", name)

		}(strconv.FormatInt(int64(ifile), 10), mfc)

		task.tileSet.M = make(maptile.Set)
		task.tileSet.Unlock()
		cliperBuffer := make(chan orb.Geometry, 16)
		go func(set maptile.Set, buffer chan<- orb.Geometry, bar *pb.ProgressBar) {
			defer close(buffer)
			for t := range set {
				bar.Increment()
				// buffer <- t.Bound()
				log.Println("starting cliper...")
				start := time.Now()
				for _, g := range layer.Collection {
					clipped := clip.Geometry(t.Bound(), g)
					secs := time.Since(start).Seconds()
					if clipped != nil {
						buffer <- clipped
						log.Printf("cliper add to buffer,time:%.4fs...", secs)
					}
				}
			}
			log.Printf("cliper buffer closing...")
			close(buffer)
		}(zoomSet, cliperBuffer, bar)

		for geom := range cliperBuffer {
			task.downloadGeom(geom, layer.Zoom)
		}
		task.wg.Wait() //wait for saving
		bar.FinishPrint(fmt.Sprintf("zoom %d finished ~", layer.Zoom))
	}
	task.Bar.FinishPrint(fmt.Sprintf("task %s finished ~", task.ID))
}
