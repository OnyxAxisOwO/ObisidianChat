package chat

import (
	"bufio"
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// Opt-in integration measurement; all clients and the server run in this test process.
func TestLoad1000Connections(t *testing.T) {
	if os.Getenv("OC_LOADTEST") != "1" {
		t.Skip("set OC_LOADTEST=1 for the local 1000-connection measurement")
	}
	f := newFixture(t)
	err := f.store.tx(t.Context(), func(tx *sql.Tx) error {
		for i := 0; i < 1000; i++ {
			uid := fmt.Sprintf("user-%04d", i)
			if _, err := tx.Exec("INSERT INTO users(id,username,name,password,role,created_at) VALUES(?,?,?,'unused','user',1)", uid, uid, uid); err != nil {
				return err
			}
			if _, err := tx.Exec("INSERT INTO sessions VALUES(?,?,?)", tokenHash(uid), uid, now()+3600000); err != nil {
				return err
			}
		}
		for i := 0; i < 10; i++ {
			room := fmt.Sprintf("group-%02d", i)
			if _, err := tx.Exec("INSERT INTO rooms(id,kind,name,owner,created_at) VALUES(?,'group',?,'user-0000',1)", room, room); err != nil {
				return err
			}
			for j := 0; j < 100; j++ {
				if _, err := tx.Exec("INSERT INTO members(room_id,user_id) VALUES(?,?)", room, fmt.Sprintf("user-%04d", i*100+j)); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	transport := &http.Transport{MaxIdleConns: 2000, MaxIdleConnsPerHost: 2000}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 45 * time.Second}
	runtime.GC()
	var baseline runtime.MemStats
	runtime.ReadMemStats(&baseline)
	readers := make([]*bufio.Reader, 1000)
	bodies := make([]io.ReadCloser, 1000)
	slots := make(chan struct{}, 50)
	errs := make(chan error, 1000)
	var wg sync.WaitGroup
	start := time.Now()
	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			req, _ := http.NewRequest("GET", f.http.URL+"/api/events", nil)
			req.AddCookie(&http.Cookie{Name: "oc_session", Value: fmt.Sprintf("user-%04d", i)})
			res, err := client.Do(req)
			if err != nil {
				errs <- err
				return
			}
			if res.StatusCode != 200 {
				res.Body.Close()
				errs <- fmt.Errorf("connect %d", res.StatusCode)
				return
			}
			reader := bufio.NewReader(res.Body)
			line, err := reader.ReadString('\n')
			if err != nil || !strings.Contains(line, "ready") {
				res.Body.Close()
				errs <- fmt.Errorf("not ready %v", err)
				return
			}
			reader.ReadString('\n')
			readers[i], bodies[i] = reader, res.Body
		}(i)
	}
	wg.Wait()
	defer func() {
		for _, body := range bodies {
			if body != nil {
				body.Close()
			}
		}
	}()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	connectTime := time.Since(start)
	runtime.GC()
	var connected runtime.MemStats
	runtime.ReadMemStats(&connected)
	deliveries := make(chan int, 1000)
	for _, reader := range readers {
		go func(reader *bufio.Reader) {
			n := 0
			for n < 10 {
				line, err := reader.ReadString('\n')
				if err != nil {
					break
				}
				if strings.Contains(line, `"type":"message"`) {
					n++
				}
			}
			deliveries <- n
		}(reader)
	}
	latency := make([]time.Duration, 100)
	errors := make(chan error, 100)
	start = time.Now()
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			group := i / 10
			uid := fmt.Sprintf("user-%04d", group*100+i%10)
			body, _ := json.Marshal(map[string]string{"body": "local load measurement", "client_id": fmt.Sprintf("load-message-%04d", i)})
			req, _ := http.NewRequest("POST", fmt.Sprintf("%s/api/rooms/group-%02d/messages", f.http.URL, group), bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.AddCookie(&http.Cookie{Name: "oc_session", Value: uid})
			sent := time.Now()
			res, err := client.Do(req)
			latency[i] = time.Since(sent)
			if err != nil {
				errors <- err
				return
			}
			data, _ := io.ReadAll(res.Body)
			res.Body.Close()
			if res.StatusCode != 200 {
				errors <- fmt.Errorf("send %d: %s", res.StatusCode, data)
			}
		}(i)
	}
	wg.Wait()
	elapsed := time.Since(start)
	close(errors)
	for err := range errors {
		t.Fatal(err)
	}
	total := 0
	for i := 0; i < 1000; i++ {
		select {
		case n := <-deliveries:
			total += n
		case <-time.After(10 * time.Second):
			t.Fatal("fanout timeout")
		}
	}
	if total != 10000 {
		t.Fatalf("delivered %d want 10000", total)
	}
	sort.Slice(latency, func(i, j int) bool { return latency[i] < latency[j] })
	connections, _, dropped := f.app.hub.stats()
	t.Logf("connections=%d; connect=%s; messages=100; delivered=%d; write_window=%s; writes/s=%.1f; POST p50=%s p95=%s p99=%s; process_heap_before=%.2f MiB; process_heap_connected=%.2f MiB; incremental_heap/client=%.2f KiB; slow_drops=%d; goroutines=%d; CPUs=%d", connections, connectTime, total, elapsed, 100/elapsed.Seconds(), latency[49], latency[94], latency[98], float64(baseline.HeapAlloc)/1048576, float64(connected.HeapAlloc)/1048576, float64(connected.HeapAlloc-baseline.HeapAlloc)/1024/1000, dropped, runtime.NumGoroutine(), runtime.NumCPU())
}

func BenchmarkHubFanout100(b *testing.B) {
	h := newHub()
	users := []string{}
	subs := []*subscription{}
	for i := 0; i < 100; i++ {
		uid := fmt.Sprint(i)
		sub, _ := h.subscribe(uid)
		users = append(users, uid)
		subs = append(subs, sub)
	}
	event := map[string]string{"type": "message", "body": "hello"}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h.publish(users, event)
		for _, sub := range subs {
			<-sub.events
		}
	}
}
