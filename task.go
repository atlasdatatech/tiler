package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/paulmach/orb"
	"github.com/paulmach/orb/clip"
	"github.com/paulmach/orb/geojson"
	"github.com/paulmach/orb/maptile"
	"github.com/paulmach/orb/maptile/tilecover"
	log "github.com/sirupsen/logrus"
	"github.com/teris-io/shortid"
	pb "gopkg.in/cheggaaa/pb.v1"
)

//MBTileVersion mbtiles版本号
const MBTileVersion = "1.2"

//Task 下载任务
type Task struct {
	ID                        string
	Name                      string
	Description               string
	File                      string
	Min                       int
	Max                       int
	Levels                    []ZoomCount
	Total                     int64
	Current                   int64
	Bar                       *pb.ProgressBar
	Geom                      orb.Geometry
	TileMap                   TileMap
	db                        *sql.DB
	workerCount               int
	bufSize                   int
	savePipeSize              int
	mergePipeSize             int
	wg                        sync.WaitGroup
	abort, pause, play, merge chan struct{}
	workers, mergingpipe      chan maptile.Tile
	savingpipe                chan Tile
	tileSet                   Set
}

//NewTask 创建下载任务
func NewTask(geom orb.Geometry, minzoom, maxzoom int, m TileMap) *Task {
	id, _ := shortid.Generate()
	task := Task{
		ID:      id,
		Name:    id,
		Geom:    geom,
		TileMap: m,
		Max:     maxzoom,
		Min:     minzoom,
	}
	for z := minzoom; z <= maxzoom; z++ {
		count := tilecover.GeometryCount(geom, maptile.Zoom(z))
		zoom := ZoomCount{
			Level: z,
			Count: count,
		}
		task.Total += count
		task.Levels = append(task.Levels, zoom)
	}

	task.abort = make(chan struct{})
	task.pause = make(chan struct{})
	task.play = make(chan struct{})
	task.merge = make(chan struct{})

	task.workerCount = cfgV.GetInt("task.workers")
	task.mergePipeSize = cfgV.GetInt("task.mergepipe")
	task.savePipeSize = cfgV.GetInt("task.savepipe")
	task.bufSize = cfgV.GetInt("task.bufsize")
	task.workers = make(chan maptile.Tile, task.workerCount)
	task.mergingpipe = make(chan maptile.Tile, task.mergePipeSize)
	task.savingpipe = make(chan Tile, task.savePipeSize)

	task.tileSet = Set{M: make(maptile.Set)}

	return &task
}

//MetaItems 输出
func (task *Task) MetaItems() map[string]string {
	b := task.Geom.Bound()
	c := b.Center()
	data := map[string]string{
		"id":          task.ID,
		"name":        task.Name,
		"description": task.Description,
		"attribution": `<a href="http://www.atlasdata.cn/" target="_blank">&copy; MapCloud</a>`,
		"basename":    task.TileMap.Name,
		"format":      task.TileMap.Format.String(),
		"type":        task.TileMap.Type,
		"pixel_scale": strconv.Itoa(TileSize),
		"version":     MBTileVersion,
		"bounds":      fmt.Sprintf(`%f,%f,%f,%f`, b.Left(), b.Bottom(), b.Right(), b.Top()),
		"center":      fmt.Sprintf(`%f,%f,%d`, c.X(), c.Y(), (task.Min+task.Max)/2),
		"minzoom":     strconv.Itoa(task.Min),
		"maxzoom":     strconv.Itoa(task.Max),
		"json":        "",
	}
	return data
}

//SetupMBTileTables 初始化配置MBTile库
func (task *Task) SetupMBTileTables() error {

	if task.File == "" {
		outdir := cfgV.GetString("output.directory")
		os.MkdirAll(outdir, os.ModePerm)
		task.File = filepath.Join(outdir, task.ID+".mbtiles")
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
		task.saveTile(tile)
	}
}

//SaveTile 保存瓦片
func (task *Task) saveTile(tile Tile) error {
	t := "files" //or files
	switch t {
	case "mbtiles":
		err := saveToMBTile(tile, task.db)
		if err != nil {
			log.Errorf(`save %v tile to mbtiles db error ~ %s`, tile.T, err)
		}
	case "files":
		err := saveToFiles(tile, filepath.Base(task.File))
		if err != nil {
			log.Errorf("create %v tile file error ~ %s\n", tile.T, err)
		}
	}

	return nil
}

//MergePipe 瓦片合并管道
func (task *Task) mergePipe() {
	select {
	case <-task.merge:
		c := len(task.mergingpipe)
		apSet := make(maptile.Set)
		for i := c; i > 0; i-- {
			t := <-task.mergingpipe
			apSet[t] = true
		}
		task.wg.Add(1)
		go task.mergeTiles(apSet)
	}
}

//MergeTiles 合并下载瓦片范围
func (task *Task) mergeTiles(set maptile.Set) {
	defer task.wg.Done()
	//merge up append set
	log.Infof("merging up appended tile set, capacity:%d ~\n", len(set))
	task.tileSet.Lock()
	task.tileSet.M = tilecover.MergeUpAppend(task.tileSet.M, set, 0)
	task.tileSet.Unlock()
}

//tileFetcher 瓦片加载器
func (task *Task) tileFetcher(t maptile.Tile) {
	defer task.wg.Done()
	defer func() {
		<-task.workers
	}()
	// start := time.Now()
	url := task.TileMap.getTileURL(t)
	resp, err := http.Get(url)
	if err != nil {
		log.Errorf(`fetch %v tile error ~ %s\n`, t, err)
		return
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Errorf(`read %v tile error ~ %s\n`, t, err)
		return
	}
	if len(body) == 0 {
		log.Errorf(`nil tile %v ~\n`, t)
		return //zero byte tiles n
	}
	tile := Tile{
		T: t,
		C: body,
	}
	saving := cfgV.GetString("task.saving")
	if saving == "on" {
		task.savingpipe <- tile
	} else {
		task.saveTile(tile)
	}
	//add to meger
	merging := cfgV.GetString("task.merging")
	if merging == "on" {
		select {
		case task.mergingpipe <- t:
		default: //mergingpipe full
			c := len(task.mergingpipe)
			apSet := make(maptile.Set)
			for i := c; i > 0; i-- {
				t := <-task.mergingpipe
				apSet[t] = true
			}
			task.wg.Add(1)
			go task.mergeTiles(apSet)
		}
	}
	// secs := time.Since(start).Seconds()
	// fmt.Printf("tile %v, %.3fs finished...\n", t, secs)
}

//DownloadZoom 下载指定层级
func (task *Task) downloadZoom(geom orb.Geometry, zoom ZoomCount) {
	bar := pb.New64(zoom.Count).Prefix(fmt.Sprintf("\nZoom %d : ", zoom.Level))
	bar.Start()

	var tilelist = make(chan maptile.Tile, task.bufSize)

	go tilecover.GeometryChannel(geom, maptile.Zoom(zoom.Level), tilelist)

	for tile := range tilelist {
		// log.Infof(`fetching tile %v ~`, tile)

		select {
		case task.workers <- tile:
			bar.Increment()
			task.Bar.Increment()
			task.wg.Add(1)
			go task.tileFetcher(tile)
		case <-task.abort:
			log.Infof(`task %s got canceled \n`, task.ID)
			close(tilelist)
		case <-task.pause:
			log.Infof(`task %s suspended \n`, task.ID)
			select {
			case <-task.play:
				log.Infof(`task %s go on \n`, task.ID)
			case <-task.abort:
				log.Infof(`task %s got canceled \n`, task.ID)
				close(tilelist)
			}
		}
	}
	task.wg.Wait()

	c := len(task.mergingpipe)
	apSet := make(maptile.Set)
	for i := c; i > 0; i-- {
		t := <-task.mergingpipe
		apSet[t] = true
	}
	task.wg.Add(1)
	go task.mergeTiles(apSet)
	task.wg.Wait()
	bar.FinishPrint(fmt.Sprintf("task %s zoom %d finished ~", task.ID, zoom.Level))
}

//Download 开启下载任务
func (task *Task) Download() {
	//g orb.Geometry, minz int, maxz int
	task.Bar = pb.New64(task.Total)
	task.Bar.Start()
	go task.mergePipe()
	for _, zoom := range task.Levels {
		task.downloadZoom(task.Geom, zoom)
	}
	task.Bar.FinishPrint(fmt.Sprintf(`task %s finished ~`, task.ID))
}

//DownloadDepth 深度优先下载
func (task *Task) DownloadDepth() {
	task.Bar = pb.New64(task.Total).Prefix("\n Fetching -> ")
	task.Bar.Start()
	for i, zoom := range task.Levels {
		task.downloadZoom(task.Geom, zoom)
		if i > 0 {
			break
		}
	}
	for i, zoom := range task.Levels {
		if i < 1 {
			continue
		}
		task.tileSet.Lock()
		zoomSet := task.tileSet.M
		mfc := task.tileSet.M.ToFeatureCollection()
		ifile := len(task.tileSet.M)
		fmt.Printf("merging up append set, %d tiles ~\n", ifile)
		fmt.Println("downloading started, Zoom:", zoom.Level, "Tiles:", ifile)
		bar := pb.StartNew(ifile).Prefix(fmt.Sprintf("\nZoom %d : ", zoom.Level))
		task.wg.Add(1)
		go func(name string, mfc *geojson.FeatureCollection) {
			defer task.wg.Done()
			data, err := json.MarshalIndent(mfc, "", " ")
			if err != nil {
				log.Printf("error marshalling json: %v\n", err)
				return
			}

			err = ioutil.WriteFile(name+".geojson", data, 0644)
			if err != nil {
				log.Printf("write file failure: %v\n", err)
			}
			log.Printf("output finished : %s.geojson\n", name)

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
				clipped := clip.Geometry(t.Bound(), task.Geom)
				secs := time.Since(start).Seconds()
				if clipped != nil {
					buffer <- clipped
					log.Printf("cliper add to buffer,time:%.4fs...\n", secs)
				}
			}
			log.Printf("cliper buffer closing...\n")
			close(buffer)
		}(zoomSet, cliperBuffer, bar)

		for geom := range cliperBuffer {
			task.downloadZoom(geom, zoom)
		}
		task.wg.Wait() //wait for saving
		bar.FinishPrint(fmt.Sprintf("zoom %d finished ~", zoom.Level))
	}
	task.Bar.FinishPrint(fmt.Sprintf("task %s finished ~", task.ID))
}
