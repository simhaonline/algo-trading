package main

import (
	"github.com/go-sql-driver/mysql"
	"reflect"
	"math"
	"database/sql"
	"time"
	"strings"
)

// NullTime is an alias for mysql.NullTime data type
type NullTime mysql.NullTime

// Scan implements the Scanner interface for NullTime
func (nt *NullTime) Scan(value interface{}) error {
	var t mysql.NullTime
	if err := t.Scan(value); err != nil {
		return err
	}

	// if nil then make Valid false
	if reflect.TypeOf(value) == nil {
		*nt = NullTime{t.Time, false}
	} else {
		*nt = NullTime{t.Time, true}
	}

	return nil
}

// NullFloat64 is an alias for sql.NullFloat64 data type
type NullFloat64 sql.NullFloat64

// Scan implements the Scanner interface for NullFloat64
func (nf *NullFloat64) Scan(value interface{}) error {
	var f sql.NullFloat64
	if err := f.Scan(value); err != nil {
		return err
	}

	// if nil then make Valid false
	if reflect.TypeOf(value) == nil {
		*nf = NullFloat64{f.Float64, false}
	} else {
		*nf = NullFloat64{f.Float64, true}
	}

	return nil
}

func Round(x, unit float64) float64 {
	if unit==0{
		return math.Round(x)
	}else {
		return math.Round(x*unit) / unit
	}
}

func FloatEquals(a, b float64) bool {

	var eps = 0.00000001

	if math.Abs(a - b) < eps {
		return true
	}
	return false
}

func ShortDuration(d time.Duration) string {
	s := d.String()
	if strings.HasSuffix(s, "m0s") {
		s = s[:len(s)-2]
	}
	if strings.HasSuffix(s, "h0m") {
		s = s[:len(s)-2]
	}
	return s
}



