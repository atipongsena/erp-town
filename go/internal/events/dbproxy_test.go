package events

import (
	"io"
	"net"
	"strconv"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

// tcpProxy forwards TCP connections to upstream. While down it closes every open
// connection and refuses new ones, which looks like a database outage to pgx.
type tcpProxy struct {
	ln       net.Listener
	upstream string
	wg       sync.WaitGroup

	mu    sync.Mutex
	down  bool
	conns map[net.Conn]struct{}
}

func newTCPProxy(t *testing.T, upstream string) *tcpProxy {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	p := &tcpProxy{ln: ln, upstream: upstream, conns: map[net.Conn]struct{}{}}
	p.wg.Add(1)
	go p.serve()
	t.Cleanup(func() {
		_ = ln.Close()
		p.closeAll()
		p.wg.Wait()
	})
	return p
}

func (p *tcpProxy) port() int { return p.ln.Addr().(*net.TCPAddr).Port }

func (p *tcpProxy) serve() {
	defer p.wg.Done()
	for {
		c, err := p.ln.Accept()
		if err != nil {
			return
		}
		if !p.isDown() {
			if up, err := net.Dial("tcp", p.upstream); err == nil && p.track(c, up) {
				p.wg.Add(2)
				go p.pipe(up, c)
				go p.pipe(c, up)
				continue
			} else if err == nil {
				_ = up.Close()
			}
		}
		_ = c.Close()
	}
}

// track registers both ends unless the proxy went down meanwhile.
func (p *tcpProxy) track(a, b net.Conn) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.down {
		return false
	}
	p.conns[a], p.conns[b] = struct{}{}, struct{}{}
	return true
}

func (p *tcpProxy) pipe(dst, src net.Conn) {
	defer p.wg.Done()
	_, _ = io.Copy(dst, src)
	_ = dst.Close()
	_ = src.Close()
	p.mu.Lock()
	delete(p.conns, dst)
	delete(p.conns, src)
	p.mu.Unlock()
}

func (p *tcpProxy) isDown() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.down
}

func (p *tcpProxy) setDown(down bool) {
	p.mu.Lock()
	p.down = down
	p.mu.Unlock()
	if down {
		p.closeAll()
	}
}

func (p *tcpProxy) closeAll() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for c := range p.conns {
		_ = c.Close()
	}
}

// proxiedPool returns a pool on the same database and schema as direct whose
// connections go through a tcpProxy, so a test can take the database away and back.
func proxiedPool(t *testing.T, direct *pgxpool.Pool) (*pgxpool.Pool, *tcpProxy) {
	t.Helper()
	cfg := direct.Config() // a copy, including the per-test search_path
	upstream := net.JoinHostPort(cfg.ConnConfig.Host, strconv.Itoa(int(cfg.ConnConfig.Port)))
	proxy := newTCPProxy(t, upstream)
	cfg.ConnConfig.Host = "127.0.0.1"
	cfg.ConnConfig.Port = uint16(proxy.port())
	cfg.ConnConfig.Fallbacks = nil
	pool, err := pgxpool.NewWithConfig(t.Context(), cfg)
	require.NoError(t, err)
	t.Cleanup(pool.Close) // runs before the proxy's cleanup (LIFO)
	return pool, proxy
}
