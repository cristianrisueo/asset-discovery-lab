package labkit

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
)

// Record is a Kafka record as tests see it.
type Record struct {
	Topic     string
	Partition int32
	Offset    int64
	Key       []byte
	Value     []byte
	Headers   map[string]string
	Timestamp time.Time
}

// Header returns a header value and whether it is present.
func (r Record) Header(name string) (string, bool) {
	v, ok := r.Headers[name]
	return v, ok
}

func fromKgo(r *kgo.Record) Record {
	h := make(map[string]string, len(r.Headers))
	for _, kv := range r.Headers {
		h[kv.Key] = string(kv.Value)
	}
	return Record{
		Topic: r.Topic, Partition: r.Partition, Offset: r.Offset,
		Key: append([]byte(nil), r.Key...), Value: append([]byte(nil), r.Value...),
		Headers: h, Timestamp: r.Timestamp,
	}
}

// Offsets maps a partition to an offset.
type Offsets map[int32]int64

// Kafka is a client for producing, reading and inspecting topics.
type Kafka struct {
	brokers []string
	cl      *kgo.Client
	adm     *kadm.Client
}

func splitBrokers(brokers string) []string {
	var out []string
	for _, b := range strings.Split(brokers, ",") {
		if b = strings.TrimSpace(b); b != "" {
			out = append(out, b)
		}
	}
	return out
}

// NewKafka builds a client. It does not connect until the first call.
func NewKafka(brokers string) (*Kafka, error) {
	bs := splitBrokers(brokers)
	if len(bs) == 0 {
		return nil, errors.New("kafka: no brokers configured")
	}
	cl, err := kgo.NewClient(
		kgo.SeedBrokers(bs...),
		kgo.DialTimeout(5*time.Second),
		kgo.RequiredAcks(kgo.AllISRAcks()),
		kgo.ProducerLinger(0),
		kgo.RecordDeliveryTimeout(30*time.Second),
	)
	if err != nil {
		return nil, fmt.Errorf("kafka at %s: %w", brokers, err)
	}
	return &Kafka{brokers: bs, cl: cl, adm: kadm.NewClient(cl)}, nil
}

// Close releases the client.
func (k *Kafka) Close() { k.cl.Close() }

func (k *Kafka) wrap(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("kafka at %s: %w", strings.Join(k.brokers, ","), err)
}

// Ping checks that a broker answers.
func (k *Kafka) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return k.wrap(k.cl.Ping(ctx))
}

func (k *Kafka) listed(ctx context.Context, end bool, topic string) (Offsets, error) {
	var (
		lo  kadm.ListedOffsets
		err error
	)
	if end {
		lo, err = k.adm.ListEndOffsets(ctx, topic)
	} else {
		lo, err = k.adm.ListStartOffsets(ctx, topic)
	}
	if err != nil {
		return nil, k.wrap(err)
	}
	if err := lo.Error(); err != nil {
		return nil, k.wrap(fmt.Errorf("list offsets of %s: %w", topic, err))
	}
	out := Offsets{}
	lo.Each(func(o kadm.ListedOffset) {
		if o.Topic == topic {
			out[o.Partition] = o.Offset
		}
	})
	if len(out) == 0 {
		return nil, k.wrap(fmt.Errorf("topic %s has no partitions", topic))
	}
	return out, nil
}

// EndOffsets returns the high watermark of every partition of topic.
func (k *Kafka) EndOffsets(ctx context.Context, topic string) (Offsets, error) {
	return k.listed(ctx, true, topic)
}

// StartOffsets returns the log start offset of every partition of topic.
func (k *Kafka) StartOffsets(ctx context.Context, topic string) (Offsets, error) {
	return k.listed(ctx, false, topic)
}

// Produce writes one record synchronously and returns it with its partition
// and offset.
func (k *Kafka) Produce(ctx context.Context, topic string, key, value []byte, headers map[string]string) (Record, error) {
	r := &kgo.Record{Topic: topic, Key: key, Value: value}
	names := make([]string, 0, len(headers))
	for n := range headers {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		r.Headers = append(r.Headers, kgo.RecordHeader{Key: n, Value: []byte(headers[n])})
	}
	res, err := k.cl.ProduceSync(ctx, r).First()
	if err != nil {
		return Record{}, k.wrap(fmt.Errorf("produce to %s: %w", topic, err))
	}
	return fromKgo(res), nil
}

// GroupLag returns the total lag of a consumer group on the given topics, and
// the lag per "topic/partition". A partition without a committed offset counts
// from the log start, because new groups start at the earliest offset (spec §8.5).
func (k *Kafka) GroupLag(ctx context.Context, group string, topics ...string) (int64, map[string]int64, error) {
	committed, err := k.adm.FetchOffsets(ctx, group)
	if err != nil {
		return 0, nil, k.wrap(fmt.Errorf("fetch offsets of group %s: %w", group, err))
	}
	if err := committed.Error(); err != nil {
		return 0, nil, k.wrap(fmt.Errorf("fetch offsets of group %s: %w", group, err))
	}
	var total int64
	per := map[string]int64{}
	for _, topic := range topics {
		end, err := k.EndOffsets(ctx, topic)
		if err != nil {
			return 0, nil, err
		}
		start, err := k.StartOffsets(ctx, topic)
		if err != nil {
			return 0, nil, err
		}
		com := Offsets{}
		for p := range end {
			if o, ok := committed.Lookup(topic, p); ok && o.Err == nil {
				com[p] = o.At
			}
		}
		for p, l := range ComputeLag(end, start, com) {
			per[fmt.Sprintf("%s/%d", topic, p)] = l
			total += l
		}
	}
	return total, per, nil
}

// ComputeLag returns the lag per partition. A missing or negative committed
// offset means "nothing committed yet": the lag is counted from the start.
func ComputeLag(end, start, committed Offsets) Offsets {
	out := Offsets{}
	for p, e := range end {
		from, ok := committed[p]
		if !ok || from < 0 {
			from = start[p]
		}
		l := e - from
		if l < 0 {
			l = 0
		}
		out[p] = l
	}
	return out
}

// Tail reads a topic forward from fixed offsets and accumulates the records.
type Tail struct {
	k       *Kafka
	topic   string
	cl      *kgo.Client
	pos     Offsets
	records []Record
}

// NewTail starts a reader of topic at from. Partitions missing from from, or
// a nil from, start at the beginning of the log.
func (k *Kafka) NewTail(ctx context.Context, topic string, from Offsets) (*Tail, error) {
	start, err := k.StartOffsets(ctx, topic)
	if err != nil {
		return nil, err
	}
	pos := Offsets{}
	parts := map[int32]kgo.Offset{}
	for p, s := range start {
		at := s
		if f, ok := from[p]; ok && f > s {
			at = f
		}
		pos[p] = at
		parts[p] = kgo.NewOffset().At(at)
	}
	cl, err := kgo.NewClient(
		kgo.SeedBrokers(k.brokers...),
		kgo.DialTimeout(5*time.Second),
		kgo.ConsumePartitions(map[string]map[int32]kgo.Offset{topic: parts}),
		kgo.FetchMaxWait(500*time.Millisecond),
	)
	if err != nil {
		return nil, k.wrap(err)
	}
	return &Tail{k: k, topic: topic, cl: cl, pos: pos}, nil
}

// TailFromEnd starts a reader at the current end of topic: it only sees
// records written after the call.
func (k *Kafka) TailFromEnd(ctx context.Context, topic string) (*Tail, error) {
	end, err := k.EndOffsets(ctx, topic)
	if err != nil {
		return nil, err
	}
	return k.NewTail(ctx, topic, end)
}

// Close releases the reader.
func (t *Tail) Close() { t.cl.Close() }

// Records returns every record read so far, in read order.
func (t *Tail) Records() []Record { return t.records }

// Positions returns the next offset to read per partition.
func (t *Tail) Positions() Offsets {
	out := Offsets{}
	for p, o := range t.pos {
		out[p] = o
	}
	return out
}

// ReadToEnd reads until every partition reaches the end offsets observed at
// the start of the call, and returns the records read by this call. A
// partition that yields nothing for 3 s while behind is treated as caught up:
// compaction can remove the last records below the end offset.
func (t *Tail) ReadToEnd(ctx context.Context) ([]Record, error) {
	end, err := t.k.EndOffsets(ctx, t.topic)
	if err != nil {
		return nil, err
	}
	var got []Record
	behind := func() []int32 {
		var ps []int32
		for p, e := range end {
			if t.pos[p] < e {
				ps = append(ps, p)
			}
		}
		return ps
	}
	for len(behind()) > 0 {
		pctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		fs := t.cl.PollFetches(pctx)
		cancel()
		if ctx.Err() != nil {
			return got, t.k.wrap(fmt.Errorf("read %s: %w", t.topic, ctx.Err()))
		}
		n := 0
		fs.EachRecord(func(r *kgo.Record) {
			n++
			rec := fromKgo(r)
			t.records = append(t.records, rec)
			got = append(got, rec)
			if r.Offset+1 > t.pos[r.Partition] {
				t.pos[r.Partition] = r.Offset + 1
			}
		})
		for _, fe := range fs.Errors() {
			if errors.Is(fe.Err, context.DeadlineExceeded) || errors.Is(fe.Err, context.Canceled) {
				continue
			}
			return got, t.k.wrap(fmt.Errorf("read %s/%d: %w", fe.Topic, fe.Partition, fe.Err))
		}
		if n == 0 {
			// Nothing arrived within 3 s although the partitions are behind:
			// the remaining range was compacted away.
			for _, p := range behind() {
				t.pos[p] = end[p]
			}
		}
	}
	return got, nil
}
