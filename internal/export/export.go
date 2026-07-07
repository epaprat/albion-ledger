// Package export renders the app's visible datasets as Excel-compatible CSV
// (feature 013). Pure: producers turn UI-facing view models into header+rows,
// Encode writes UTF-8 BOM + CRLF + RFC 4180 quoting. Dialogs and file IO live
// in the wails adapter — this package never touches the filesystem itself.
package export

import (
	"encoding/csv"
	"fmt"
	"io"
	"time"
)

// utf8BOM makes Excel (notably on Windows/TR locales) read the file as UTF-8
// instead of guessing a legacy codepage and mangling accented characters.
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// Encode writes one CSV document: BOM, header, then rows. CRLF line endings and
// RFC 4180 quoting come from encoding/csv — fields containing separators,
// quotes or newlines are quoted with inner quotes doubled. An empty rows slice
// still yields a valid BOM+header document (contract §1.3).
func Encode(w io.Writer, header []string, rows [][]string) error {
	if _, err := w.Write(utf8BOM); err != nil {
		return err
	}
	cw := csv.NewWriter(w)
	cw.UseCRLF = true
	if err := cw.Write(header); err != nil {
		return err
	}
	for _, row := range rows {
		if err := cw.Write(row); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

// Filename names one export: albion-<key>-<YYYYMMDD-HHMMSS>.csv — sortable,
// collision-free, origin obvious (research R-6).
func Filename(key string, t time.Time) string {
	return fmt.Sprintf("albion-%s-%s.csv", key, t.Format("20060102-150405"))
}
