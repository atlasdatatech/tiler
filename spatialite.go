package main

import (
	"database/sql"

	"github.com/shaxbee/go-spatialite/wkb"
	log "github.com/sirupsen/logrus"
)

func testPoint() {
	db := initDB()
	defer db.Close()

	_, err := db.Exec("CREATE TABLE poi(title TEXT)")
	if err != nil {
		log.Println(err)
	}

	_, err = db.Exec("SELECT AddGeometryColumn('poi', 'loc', 4326, 'POINT')")
	if err != nil {
		log.Println(err)
	}

	p1 := wkb.Point{X: 10, Y: 10}
	_, err = db.Exec("INSERT INTO poi(title, loc) VALUES (?, ST_PointFromWKB(?, 4326))", "foo", p1)
	if err != nil {
		log.Println(err)
	}
	p2 := wkb.Point{}
	r := db.QueryRow("SELECT ST_AsBinary(loc) AS loc FROM poi WHERE title=?", "foo")
	var gj string
	r1 := db.QueryRow("SELECT AsGeoJSON(loc) AS loc FROM poi WHERE title=?", "foo")
	err = r1.Scan(&gj)
	log.Println(gj)
	err = r.Scan(&p2)
	if err != nil {
		log.Println(err)
	}
	log.Println(p1.Equal(p2))
}

func initDB() *sql.DB {
	db, err := sql.Open("spatialite", "file:dummy.db?mode=memory&cache=shared")
	if err != nil {
		log.Println(err)
	}

	_, err = db.Exec("SELECT InitSpatialMetadata()")
	if err != nil {
		log.Println(err)
	}
	return db
}
