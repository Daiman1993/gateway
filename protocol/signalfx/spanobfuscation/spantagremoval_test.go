package spanobfuscation

import (
	"context"
	"testing"

	"github.com/gobwas/glob"
	"github.com/gobwas/glob/match"
	"github.com/signalfx/golib/datapoint"
	"github.com/signalfx/golib/event"
	"github.com/signalfx/golib/pointer"
	"github.com/signalfx/golib/trace"
	. "github.com/smartystreets/goconvey/convey"
)

type end struct {
}

func (e *end) AddSpans(ctx context.Context, spans []*trace.Span) error {
	return nil
}

func (e *end) AddDatapoints(ctx context.Context, points []*datapoint.Datapoint) error {
	return nil
}

func (e *end) AddEvents(ctx context.Context, events []*event.Event) error {
	return nil
}

func TestNew(t *testing.T) {
	defaultGlob, _ := glob.Compile(`*`)
	serviceGlob, _ := glob.Compile(`\^\\some*service\$`)
	opGlob, _ := glob.Compile(`operation\.*`)

	var cases = []struct {
		desc        string
		config      []*TagRemovalRuleConfig
		outputRules []*rule
	}{
		{
			desc:        "empty service and empty operation",
			config:      []*TagRemovalRuleConfig{{Tags: []string{"test-tag"}}},
			outputRules: []*rule{{service: defaultGlob, operation: defaultGlob, tags: []string{"test-tag"}}},
		},
		{
			desc:        "service regex and empty operation",
			config:      []*TagRemovalRuleConfig{{Service: pointer.String(`^\some*service$`), Tags: []string{"test-tag"}}},
			outputRules: []*rule{{service: serviceGlob, operation: defaultGlob, tags: []string{"test-tag"}}},
		},
		{
			desc:        "empty service and operation regex",
			config:      []*TagRemovalRuleConfig{{Operation: pointer.String(`operation.*`), Tags: []string{"test-tag"}}},
			outputRules: []*rule{{service: defaultGlob, operation: opGlob, tags: []string{"test-tag"}}},
		},
		{
			desc:        "service regex and operation regex",
			config:      []*TagRemovalRuleConfig{{Service: pointer.String(`^\some*service$`), Operation: pointer.String(`operation.*`), Tags: []string{"test-tag"}}},
			outputRules: []*rule{{service: serviceGlob, operation: opGlob, tags: []string{"test-tag"}}},
		},
		{
			desc:        "multiple tags",
			config:      []*TagRemovalRuleConfig{{Service: pointer.String(`^\some*service$`), Operation: pointer.String(`operation.*`), Tags: []string{"test-tag", "another-tag"}}},
			outputRules: []*rule{{service: serviceGlob, operation: opGlob, tags: []string{"test-tag", "another-tag"}}},
		},
		{
			desc: "multiple rules",
			config: []*TagRemovalRuleConfig{
				{Tags: []string{"test-tag"}},
				{Service: pointer.String(`^\some*service$`), Tags: []string{"test-tag"}},
				{Operation: pointer.String(`operation.*`), Tags: []string{"test-tag"}},
				{Service: pointer.String(`^\some*service$`), Operation: pointer.String(`operation.*`), Tags: []string{"test-tag"}},
			},
			outputRules: []*rule{
				{service: defaultGlob, operation: defaultGlob, tags: []string{"test-tag"}},
				{service: serviceGlob, operation: defaultGlob, tags: []string{"test-tag"}},
				{service: defaultGlob, operation: opGlob, tags: []string{"test-tag"}},
				{service: serviceGlob, operation: opGlob, tags: []string{"test-tag"}},
			},
		},
	}
	Convey("we should create a valid SpanTagRemoval with", t, func() {
		for _, tc := range cases {
			Convey(tc.desc, func() {
				e := &end{}
				so, err := New(tc.config, e)
				So(err, ShouldBeNil)
				So(so, ShouldNotBeNil)
				for i := 0; i < len(so.rules); i++ {
					So(so.rules[i].service.(match.Matcher).String(), ShouldEqual, tc.outputRules[i].service.(match.Matcher).String())
					So(so.rules[i].operation.(match.Matcher).String(), ShouldEqual, tc.outputRules[i].operation.(match.Matcher).String())
					for idx, tag := range so.rules[i].tags {
						So(tag, ShouldEqual, tc.outputRules[i].tags[idx])
					}
				}
			})
		}
	})
}

func TestTagDelete(t *testing.T) {
	Convey("Given a SpanTagRemoval config", t, func() {
		config := []*TagRemovalRuleConfig{
			{
				Service: pointer.String("test-service"),
				Tags:    []string{"delete-me"},
			},
			{
				Service:   pointer.String("some*service"),
				Operation: pointer.String("sensitive*"),
				Tags:      []string{"PII", "SSN"},
			},
		}
		so, _ := New(config, &end{})
		Convey("should remove tag from exact-match service", func() {
			spans := []*trace.Span{makeSpan("test-service", "shouldn't matter", map[string]string{"delete-me": "val"})}
			so.AddSpans(context.Background(), spans)
			So(spans[0].Tags, ShouldResemble, map[string]string{})
		})
		Convey("should not remove tag from exact-match service as prefix", func() {
			spans := []*trace.Span{makeSpan("false-test-service", "shouldn't matter", map[string]string{"delete-me": "val"})}
			so.AddSpans(context.Background(), spans)
			So(spans[0].Tags, ShouldResemble, map[string]string{"delete-me": "val"})
		})
		Convey("should not remove tag from exact-match service as suffix", func() {
			spans := []*trace.Span{makeSpan("test-service-extra", "shouldn't matter", map[string]string{"delete-me": "val"})}
			so.AddSpans(context.Background(), spans)
			So(spans[0].Tags, ShouldResemble, map[string]string{"delete-me": "val"})
		})
		Convey("should remove tag from matching wildcard service and operation", func() {
			spans := []*trace.Span{makeSpan("some-test-service", "sensitive-data-leak", map[string]string{"PII": "val"})}
			so.AddSpans(context.Background(), spans)
			So(spans[0].Tags, ShouldResemble, map[string]string{})
		})
		Convey("should not remove tag with mismatched tag name", func() {
			spans := []*trace.Span{makeSpan("some-test-service", "sensitive-data-leak", map[string]string{"delete-me": "val"})}
			so.AddSpans(context.Background(), spans)
			So(spans[0].Tags, ShouldResemble, map[string]string{"delete-me": "val"})
		})
		Convey("should not remove tag with matching service but unmatched operation", func() {
			spans := []*trace.Span{makeSpan("some-test-service", "secure-op", map[string]string{"PII": "val"})}
			so.AddSpans(context.Background(), spans)
			So(spans[0].Tags, ShouldResemble, map[string]string{"PII": "val"})
		})
		Convey("should remove all tags defined in the removal rule", func() {
			spans := []*trace.Span{makeSpan("some-test-service", "sensitive-data-leak", map[string]string{"PII": "val", "SSN": "111-22-3333"})}
			so.AddSpans(context.Background(), spans)
			So(spans[0].Tags, ShouldResemble, map[string]string{})
		})
		Convey("should handle an empty span", func() {
			spans := []*trace.Span{{}}
			err := so.AddSpans(context.Background(), spans)
			So(err, ShouldBeNil)
		})
		Convey("should handle a span with an empty service", func() {
			spans := []*trace.Span{{LocalEndpoint: &trace.Endpoint{}}}
			err := so.AddSpans(context.Background(), spans)
			So(err, ShouldBeNil)
		})
	})
}

func TestNewBad(t *testing.T) {
	Convey("test missing tags", t, func() {
		_, err := New([]*TagRemovalRuleConfig{{}}, &end{})
		So(err, ShouldNotBeNil)
	})
	Convey("test empty tag name", t, func() {
		_, err := New([]*TagRemovalRuleConfig{{Tags: []string{""}}}, &end{})
		So(err, ShouldNotBeNil)
	})
	Convey("test empty tags array", t, func() {
		_, err := New([]*TagRemovalRuleConfig{{Tags: []string{}}}, &end{})
		So(err, ShouldNotBeNil)
	})
}

func TestPassthrough(t *testing.T) {
	Convey("test passthroughs", t, func() {
		so := &SpanTagRemoval{next: &end{}}
		So(so.AddDatapoints(context.Background(), []*datapoint.Datapoint{}), ShouldBeNil)
		So(so.AddEvents(context.Background(), []*event.Event{}), ShouldBeNil)
	})
}

func TestEscapeMetaCharacters(t *testing.T) {
	var cases = []struct {
		desc    string
		pattern string
		match   []string
		noMatch []string
	}{
		{
			desc:    `^`,
			pattern: `^test*service`,
			match:   []string{`^test-service`, `^test^service`},
			noMatch: []string{`test-service`, `testservice`},
		},
		{
			desc:    `?`,
			pattern: `test?service*`,
			match:   []string{`test?service-one`},
			noMatch: []string{`test-service`, `test.service`, `testaservice`, `test.service.prod`},
		},
		{
			desc:    `\`,
			pattern: `test\*service`,
			match:   []string{`test\this\service`, `test\service`, `test\\service`},
			noMatch: []string{`test-service`, `test.service`, `testservice`},
		},
		{
			desc:    `{}`,
			pattern: `service{2}`,
			match:   []string{`service{2}`},
			noMatch: []string{`servicee`, `serviceeee`},
		},
		{
			desc:    `[]`,
			pattern: `service*[a-z]`,
			match:   []string{`service[a-z]`, `service/handle/[a-z]`},
			noMatch: []string{`servicea`, `servicee`},
		},
	}
	Convey("should correctly handle special character ", t, func() {
		for _, c := range cases {
			Convey(c.desc, func() {
				g := getGlob(c.pattern)
				for _, m := range c.match {
					So(g.Match(m), ShouldBeTrue)
				}
				for _, n := range c.noMatch {
					So(g.Match(n), ShouldBeFalse)
				}
			})
		}
	})
}

func BenchmarkOne(b *testing.B) {
	spans := make([]*trace.Span, 0, b.N)
	for i := 0; i < b.N; i++ {
		spans = append(spans, makeSpan("some-test-service", "test-op", map[string]string{"PII": "name", "otherTag": "ok"}))
	}
	config := []*TagRemovalRuleConfig{
		{
			Service:   pointer.String("some*test*service"),
			Operation: pointer.String("test*"),
			Tags:      []string{"PII"},
		},
	}
	so, _ := New(config, &end{})
	b.ResetTimer()
	b.ReportAllocs()
	so.AddSpans(context.Background(), spans)
}

func BenchmarkTen(b *testing.B) {
	spans := make([]*trace.Span, 0, b.N)
	for i := 0; i < b.N; i++ {
		spans = append(spans, makeSpan("some-test-service", "test-op", map[string]string{"PII": "name", "otherTag": "ok"}))
	}
	config := make([]*TagRemovalRuleConfig, 0, 10)
	for i := 0; i < 9; i++ {
		rule := &TagRemovalRuleConfig{
			Service:   pointer.String("some*test*service" + string(i)),
			Operation: pointer.String("test*"),
			Tags:      []string{"PII"},
		}
		config = append(config, rule)
	}
	config = append(config, &TagRemovalRuleConfig{
		Service:   pointer.String("some*test*service"),
		Operation: pointer.String("test*"),
		Tags:      []string{"PII"},
	})
	so, _ := New(config, &end{})
	b.ResetTimer()
	b.ReportAllocs()
	so.AddSpans(context.Background(), spans)
}

func makeSpan(service string, operation string, tags map[string]string) *trace.Span {
	localEndpoint := &trace.Endpoint{
		ServiceName: pointer.String(service),
	}
	return &trace.Span{
		Name:          pointer.String(operation),
		LocalEndpoint: localEndpoint,
		Tags:          tags,
	}
}