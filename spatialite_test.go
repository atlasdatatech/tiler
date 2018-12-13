package main

import (
	"database/sql"
	"testing"

	"github.com/shaxbee/go-spatialite/wkb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPoint(t *testing.T) {
	db := makeDB(t)
	defer db.Close()

	_, err := db.Exec("CREATE TABLE poi(title TEXT)")
	require.NoError(t, err)

	_, err = db.Exec("SELECT AddGeometryColumn('poi', 'loc', 4326, 'POINT')")
	require.NoError(t, err)

	p1 := wkb.Point{X: 10, Y: 10}
	_, err = db.Exec("INSERT INTO poi(title, loc) VALUES (?, ST_PointFromWKB(?, 4326))", "foo", p1)
	assert.NoError(t, err)

	p2 := wkb.Point{}
	r := db.QueryRow("SELECT ST_AsBinary(loc) AS loc FROM poi WHERE title=?", "foo")
	if err := r.Scan(&p2); assert.NoError(t, err) {
		assert.Equal(t, p1, p2)
	}
}

func makeDB(t *testing.T) *sql.DB {
	db, err := sql.Open("spatialite", "file:dummy.db?mode=memory&cache=shared")
	require.NoError(t, err)

	_, err = db.Exec("SELECT InitSpatialMetadata()")
	require.NoError(t, err)
	return db
}
