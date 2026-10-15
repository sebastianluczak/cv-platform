package jetstream

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

const (
	pilotAckWait       = 2 * time.Second
	pilotMaxDeliveries = 4
)

func TestJetStreamPilotHandoffAndRedelivery(t *testing.T) {
	url := os.Getenv("NATS_URL")
	if url == "" {
		t.Skip("set NATS_URL to run the JetStream integration pilot")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	conn, err := nats.Connect(url, nats.Timeout(2*time.Second))
	if err != nil {
		t.Fatalf("connect to NATS: %v", err)
	}
	t.Cleanup(conn.Close)

	js, err := jetstream.New(conn)
	if err != nil {
		t.Fatalf("create JetStream client: %v", err)
	}

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	subjectPrefix := "cv.platform.pilot." + suffix
	workStream := "CVPLATFORM_PILOT_WORK_" + suffix
	eventStream := "CVPLATFORM_PILOT_EVENTS_" + suffix
	failedStream := "CVPLATFORM_PILOT_FAILED_" + suffix
	streams := []jetstream.StreamConfig{
		{
			Name: workStream, Subjects: []string{subjectPrefix + ".jobs.>"},
			Retention: jetstream.WorkQueuePolicy, Storage: jetstream.FileStorage,
			MaxAge: time.Hour, Duplicates: 2 * time.Minute,
		},
		{
			Name: eventStream, Subjects: []string{subjectPrefix + ".events"},
			Retention: jetstream.LimitsPolicy, Storage: jetstream.FileStorage,
			MaxAge: 24 * time.Hour,
		},
		{
			Name: failedStream, Subjects: []string{subjectPrefix + ".failed.>"},
			Retention: jetstream.LimitsPolicy, Storage: jetstream.FileStorage,
			MaxAge: 7 * 24 * time.Hour,
		},
	}
	createdStreams := make([]string, 0, len(streams))
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), time.Second)
		defer cleanupCancel()
		for _, name := range createdStreams {
			if err := js.DeleteStream(cleanupCtx, name); err != nil {
				t.Errorf("delete pilot stream %q: %v", name, err)
			}
		}
	})
	for _, stream := range streams {
		createPilotStream(t, ctx, js, stream)
		createdStreams = append(createdStreams, stream.Name)
	}

	extractionSubject := subjectPrefix + ".jobs.extract"
	extractionConsumer := createPilotConsumer(t, ctx, js, workStream, "pilot-extractor", extractionSubject)
	job := []byte(`{"id":"pilot-job","bucket":"cv-platform-uploads","object_key":"uploads/pilot-job","content_type":"application/pdf"}`)
	publishPilotMessage(t, ctx, js, extractionSubject, job, "pilot-job:extract")

	first := nextPilotMessage(t, extractionConsumer)
	if got := string(first.Data()); got != string(job) {
		t.Fatalf("extraction payload = %s, want %s", got, job)
	}
	if err := first.NakWithDelay(10 * time.Millisecond); err != nil {
		t.Fatalf("schedule extraction redelivery: %v", err)
	}

	retried := nextPilotMessage(t, extractionConsumer)
	metadata, err := retried.Metadata()
	if err != nil {
		t.Fatalf("read redelivered message metadata: %v", err)
	}
	if metadata.NumDelivered != 2 {
		t.Fatalf("delivery count = %d, want 2", metadata.NumDelivered)
	}

	redactionSubject := subjectPrefix + ".jobs.pii"
	redactionConsumer := createPilotConsumer(t, ctx, js, workStream, "pilot-piistripper", redactionSubject)
	redactionJob := []byte(`{"id":"pilot-job","bucket":"cv-platform-uploads","object_key":"results/pilot-job.txt"}`)
	publishPilotMessage(t, ctx, js, redactionSubject, redactionJob, "pilot-job:pii")
	redactionMessage := nextPilotMessage(t, redactionConsumer)
	if got := string(redactionMessage.Data()); got != string(redactionJob) {
		t.Fatalf("redaction payload = %s, want %s", got, redactionJob)
	}
	if err := redactionMessage.Ack(); err != nil {
		t.Fatalf("acknowledge redaction job: %v", err)
	}
	if err := retried.Ack(); err != nil {
		t.Fatalf("acknowledge extraction after confirmed handoff: %v", err)
	}

	for _, stage := range []struct {
		durable string
		subject string
		payload []byte
		message string
	}{
		{"pilot-skillmatcher", subjectPrefix + ".jobs.skills", []byte(`{"id":"pilot-job","object_key":"results/pilot-job.redacted.txt"}`), "pilot-job:skills"},
		{"pilot-offermatcher", subjectPrefix + ".jobs.offers", []byte(`{"id":"pilot-offer","offer_text":"required skills"}`), "pilot-offer"},
		{"pilot-aiweighter", subjectPrefix + ".jobs.aiweight", []byte(`{"weigh_id":"pilot-weigh"}`), "pilot-aiweight"},
	} {
		consumer := createPilotConsumer(t, ctx, js, workStream, stage.durable, stage.subject)
		publishPilotMessage(t, ctx, js, stage.subject, stage.payload, stage.message)
		message := nextPilotMessage(t, consumer)
		if got := string(message.Data()); got != string(stage.payload) {
			t.Fatalf("%s payload = %s, want %s", stage.durable, got, stage.payload)
		}
		if err := message.Ack(); err != nil {
			t.Fatalf("acknowledge %s message: %v", stage.durable, err)
		}
	}

	publishPilotMessage(t, ctx, js, subjectPrefix+".events", []byte(`{"job_id":"pilot-job","stage":"processing"}`), "pilot-event")
	publishPilotMessage(t, ctx, js, subjectPrefix+".failed.extract", []byte(`{"id":"pilot-job","reason":"retry limit reached"}`), "pilot-failed")

	retryJob := []byte(`{"id":"retry-limit-job","bucket":"cv-platform-uploads","object_key":"uploads/retry-limit-job","content_type":"application/pdf"}`)
	publishPilotMessage(t, ctx, js, extractionSubject, retryJob, "retry-limit-job:extract")
	for attempt := 1; attempt <= pilotMaxDeliveries; attempt++ {
		message := nextPilotMessage(t, extractionConsumer)
		metadata, err := message.Metadata()
		if err != nil {
			t.Fatalf("read retry metadata: %v", err)
		}
		if metadata.NumDelivered != uint64(attempt) {
			t.Fatalf("retry delivery count = %d, want %d", metadata.NumDelivered, attempt)
		}
		if attempt < pilotMaxDeliveries {
			if err := message.NakWithDelay(10 * time.Millisecond); err != nil {
				t.Fatalf("schedule retry %d: %v", attempt, err)
			}
			continue
		}
		publishPilotMessage(t, ctx, js, subjectPrefix+".failed.extract", []byte(`{"id":"retry-limit-job","reason":"max deliveries reached"}`), "retry-limit-job:failed")
		if err := message.Ack(); err != nil {
			t.Fatalf("acknowledge job after publishing terminal failure: %v", err)
		}
	}

	work, err := js.Stream(ctx, workStream)
	if err != nil {
		t.Fatalf("look up work stream: %v", err)
	}
	workInfo, err := work.Info(ctx)
	if err != nil {
		t.Fatalf("inspect work stream: %v", err)
	}
	if workInfo.Config.Retention != jetstream.WorkQueuePolicy ||
		workInfo.Config.Storage != jetstream.FileStorage ||
		workInfo.Config.MaxAge != time.Hour {
		t.Fatalf("work stream config = %+v, want file-backed work-queue retention with one-hour max age", workInfo.Config)
	}

	for _, test := range []struct {
		stream   string
		subject  string
		messages uint64
		maxAge   time.Duration
	}{
		{eventStream, subjectPrefix + ".events", 1, 24 * time.Hour},
		{failedStream, subjectPrefix + ".failed.>", 2, 7 * 24 * time.Hour},
	} {
		stream, err := js.Stream(ctx, test.stream)
		if err != nil {
			t.Fatalf("look up %s stream: %v", test.stream, err)
		}
		info, err := stream.Info(ctx)
		if err != nil {
			t.Fatalf("inspect %s stream: %v", test.stream, err)
		}
		if info.State.Msgs != test.messages || info.Config.Subjects[0] != test.subject {
			t.Fatalf("%s stream state = %+v, want %d messages for %s", test.stream, info.State, test.messages, test.subject)
		}
		if info.Config.Retention != jetstream.LimitsPolicy ||
			info.Config.Storage != jetstream.FileStorage ||
			info.Config.MaxAge != test.maxAge {
			t.Fatalf("%s stream config = %+v, want file-backed limits retention with max age %s", test.stream, info.Config, test.maxAge)
		}
	}
}

func createPilotStream(t *testing.T, ctx context.Context, js jetstream.JetStream, config jetstream.StreamConfig) {
	t.Helper()
	if _, err := js.CreateStream(ctx, config); err != nil {
		t.Fatalf("create pilot stream %q: %v", config.Name, err)
	}
}

func createPilotConsumer(
	t *testing.T,
	ctx context.Context,
	js jetstream.JetStream,
	streamName, durable, subject string,
) jetstream.Consumer {
	t.Helper()
	consumer, err := js.CreateConsumer(ctx, streamName, jetstream.ConsumerConfig{
		Durable: durable, FilterSubject: subject, AckPolicy: jetstream.AckExplicitPolicy,
		AckWait: pilotAckWait, MaxDeliver: pilotMaxDeliveries, MaxAckPending: 1,
	})
	if err != nil {
		t.Fatalf("create durable consumer %q: %v", durable, err)
	}
	info, err := consumer.Info(ctx)
	if err != nil {
		t.Fatalf("inspect durable consumer %q: %v", durable, err)
	}
	if info.Config.AckPolicy != jetstream.AckExplicitPolicy ||
		info.Config.AckWait != pilotAckWait ||
		info.Config.MaxDeliver != pilotMaxDeliveries ||
		info.Config.MaxAckPending != 1 {
		t.Fatalf("consumer %q config = %+v, want explicit ack, %s ack wait, %d deliveries, and max pending 1",
			durable, info.Config, pilotAckWait, pilotMaxDeliveries)
	}
	return consumer
}

func publishPilotMessage(
	t *testing.T,
	ctx context.Context,
	js jetstream.JetStream,
	subject string,
	payload []byte,
	messageID string,
) {
	t.Helper()
	if !json.Valid(payload) {
		t.Fatalf("pilot payload for %s is not valid JSON", subject)
	}
	if _, err := js.Publish(ctx, subject, payload, jetstream.WithMsgID(messageID)); err != nil {
		t.Fatalf("publish to %s and await JetStream acknowledgement: %v", subject, err)
	}
}

func nextPilotMessage(t *testing.T, consumer jetstream.Consumer) jetstream.Msg {
	t.Helper()
	batch, err := consumer.Fetch(1, jetstream.FetchMaxWait(2*time.Second))
	if err != nil {
		t.Fatalf("fetch JetStream message: %v", err)
	}
	var received jetstream.Msg
	for message := range batch.Messages() {
		received = message
	}
	if err := batch.Error(); err != nil {
		t.Fatalf("read JetStream message batch: %v", err)
	}
	if received == nil {
		t.Fatal("JetStream fetch returned no message")
	}
	return received
}
