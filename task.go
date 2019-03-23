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
		Min:     m.Min,
		Max:     m.Max,
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
	bound := orb.Bound{Min: orb.Point{1, 1}, Max: orb.Point{-1, -1}}
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
	bound := orb.Bound{Min: orb.Point{1, 1}, Max: orb.Point{-1, -1}}
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
		"json":        task.TileMap.JSON,
	}
	return data
}

//SetupMBTileTables 初始化配置MBTile库
func (task *Task) SetupMBTileTables() error {

	if task.File == "" {
		outdir := viper.GetString("output.directory")
		os.MkdirAll(outdir, os.ModePerm)
		task.File = filepath.Join(outdir, fmt.Sprintf("%s%d-%d.%s.mbtiles", task.Name, task.Min, task.Max, task.ID))
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
			if strings.HasPrefix(err.Error(), "UNIQUE constraint failed") {
				log.Warnf("save %v tile to mbtiles db error ~ %s", tile.T, err)
			} else {
				log.Errorf("save %v tile to mbtiles db error ~ %s", tile.T, err)
			}
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
