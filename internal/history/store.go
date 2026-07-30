package history

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/1945251yyq/qnap-monitor-one/internal/monitor"
	bolt "go.etcd.io/bbolt"
)

var pointsBucket = []byte("points")

type Store struct {
	db        *bolt.DB
	retention time.Duration
}

func Open(dataRoot string, retention time.Duration) (*Store, error) {
	if err := os.MkdirAll(dataRoot, 0o750); err != nil {
		return nil, fmt.Errorf("创建数据目录: %w", err)
	}
	db, err := bolt.Open(filepath.Join(dataRoot, "history.db"), 0o600, &bolt.Options{Timeout: 2 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("打开历史数据库: %w", err)
	}
	store := &Store{db: db, retention: retention}
	if err := db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(pointsBucket)
		return err
	}); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) Add(snapshot monitor.Snapshot) error {
	point := monitor.HistoryPoint{
		Time: snapshot.Timestamp.Unix(), CPU: snapshot.System.CPUPercent,
		Memory: snapshot.System.MemoryPercent, CPUTemp: snapshot.System.CPUTemperature,
		SystemTemp: snapshot.System.Temperature,
	}
	for _, network := range snapshot.Networks {
		point.NetworkRx += network.RxPerSec
		point.NetworkTx += network.TxPerSec
	}
	if len(snapshot.Volumes) > 0 {
		point.VolumePercent = snapshot.Volumes[0].Percent
	}
	value, err := json.Marshal(point)
	if err != nil {
		return err
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(pointsBucket)
		key := make([]byte, 8)
		binary.BigEndian.PutUint64(key, uint64(point.Time))
		if err := bucket.Put(key, value); err != nil {
			return err
		}
		cutoff := uint64(time.Now().Add(-s.retention).Unix())
		cursor := bucket.Cursor()
		for key, _ := cursor.First(); key != nil && binary.BigEndian.Uint64(key) < cutoff; key, _ = cursor.Next() {
			if err := cursor.Delete(); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Store) Query(since time.Time, limit int) ([]monitor.HistoryPoint, error) {
	if limit < 1 || limit > 10000 {
		limit = 5000
	}
	var result []monitor.HistoryPoint
	err := s.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(pointsBucket)
		cursor := bucket.Cursor()
		start := make([]byte, 8)
		binary.BigEndian.PutUint64(start, uint64(since.Unix()))
		for key, value := cursor.Seek(start); key != nil && len(result) < limit; key, value = cursor.Next() {
			var point monitor.HistoryPoint
			if json.Unmarshal(value, &point) == nil {
				result = append(result, point)
			}
		}
		return nil
	})
	return result, err
}
