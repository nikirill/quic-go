package quic

type sender interface {
	Send(p *packetBuffer)
	Run() error
	WouldBlock() bool
	Available() <-chan struct{}
	Close()
}

type sendQueue struct {
	queue       chan *packetBuffer
	closeCalled chan struct{} // runStopped when Close() is called
	runStopped  chan struct{} // runStopped when the run loop returns
	available   chan struct{}
	conn        sendConn
	//interFile   *os.File
	//timestamps  TimeQueue
}

var _ sender = &sendQueue{}

const sendQueueCapacity = 8

//type TimeQueue struct {
//	sync.Mutex
//	items []time.Time
//}
//
//func (q *TimeQueue) Enqueue(item time.Time) {
//	q.Lock()
//	defer q.Unlock()
//	q.items = append(q.items, item)
//}
//
//func (q *TimeQueue) Dequeue() time.Time {
//	q.Lock()
//	defer q.Unlock()
//	item := q.items[0]
//	q.items = q.items[1:]
//	return item
//}

func newSendQueue(conn sendConn) sender {
	return &sendQueue{
		conn:        conn,
		runStopped:  make(chan struct{}),
		closeCalled: make(chan struct{}),
		available:   make(chan struct{}, 1),
		queue:       make(chan *packetBuffer, sendQueueCapacity),
	}
}

//func newSendQueue(conn sendConn) sender {
//	q := &sendQueue{
//		conn:        conn,
//		runStopped:  make(chan struct{}),
//		closeCalled: make(chan struct{}),
//		available:   make(chan struct{}, 1),
//		queue:       make(chan *packetBuffer, sendQueueCapacity),
//	}
//	q.interFile, _ = os.OpenFile("interpkt.csv", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0666)
//	return q
//}

// Send sends out a packet. It's guaranteed to not block.
// Callers need to make sure that there's actually space in the send queue by calling WouldBlock.
// Otherwise Send will panic.
func (h *sendQueue) Send(p *packetBuffer) {
	select {
	case h.queue <- p:
		//h.timestamps.Enqueue(time.Now())
		// clear available channel if we've reached capacity
		if len(h.queue) == sendQueueCapacity {
			select {
			case <-h.available:
			default:
			}
		}
	case <-h.runStopped:
	default:
		panic("sendQueue.Send would have blocked")
	}
}

func (h *sendQueue) WouldBlock() bool {
	return len(h.queue) == sendQueueCapacity
}

func (h *sendQueue) Available() <-chan struct{} {
	return h.available
}

func (h *sendQueue) Run() error {
	defer close(h.runStopped)
	var shouldClose bool
	for {
		if shouldClose && len(h.queue) == 0 {
			return nil
		}
		select {
		case <-h.closeCalled:
			h.closeCalled = nil // prevent this case from being selected again
			// make sure that all queued packets are actually sent out
			shouldClose = true
			//h.interFile.Close()
		case p := <-h.queue:
			//before := time.Now()
			if err := h.conn.Write(p.Data); err != nil {
				// This additional check enables:
				// 1. Checking for "datagram too large" message from the kernel, as such,
				// 2. Path MTU discovery,and
				// 3. Eventual detection of loss PingFrame.
				if !isMsgSizeErr(err) {
					return err
				}
			}
			//h.interFile.WriteString(fmt.Sprintf("%.7f, ", time.Since(before).Seconds()))
			p.Release()
			select {
			case h.available <- struct{}{}:
			default:
			}
		}
	}
}

func (h *sendQueue) Close() {
	close(h.closeCalled)
	// wait until the run loop returned
	<-h.runStopped
}
