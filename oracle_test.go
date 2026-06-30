// Copyright (c) the go-ruby-net-imap/net-imap authors
//
// SPDX-License-Identifier: BSD-3-Clause

package imap

import (
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

// rubyBin locates a usable `ruby` once. The oracle tests skip themselves when it
// is absent (the qemu cross-arch lanes and the Windows lane), so the deterministic
// suite alone drives the 100% gate there.
func rubyBin(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("ruby")
	if err != nil {
		t.Skip("ruby not on PATH; skipping MRI oracle")
	}
	return path
}

// requireSASLFactory skips the test when the bundled net-imap predates the
// Net::IMAP::SASL.authenticator factory (the gem the CI ubuntu/macos runners
// bundle can be older than the locally-installed one). The pure-Go SASL parity
// is fully proven by the deterministic golden vectors regardless.
func requireSASLFactory(t *testing.T, bin string) {
	t.Helper()
	out, err := exec.Command(bin, "-rnet/imap", "-e",
		`print(Net::IMAP::SASL.respond_to?(:authenticator) ? "1" : "0")`).CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "1" {
		t.Skip("net-imap lacks Net::IMAP::SASL.authenticator; deterministic golden vectors cover SASL")
	}
}

// rubyEval runs a Ruby script (with net/imap required) and returns its stdout.
// The script $stdout.binmode's itself so Windows text-mode never pollutes the
// bytes (the go-ruby-erb lesson).
func rubyEval(t *testing.T, bin, script string) string {
	t.Helper()
	cmd := exec.Command(bin, "-rnet/imap", "-e", "$stdout.binmode\n"+script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("ruby error: %v\nscript:\n%s\noutput:\n%s", err, script, out)
	}
	return string(out)
}

// TestOracleCommandBytes checks this package emits the exact command octets MRI's
// Net::IMAP#send_command would write to the socket, for a representative span of
// commands and argument-encoding shapes (atom / quoted / literal / list).
func TestOracleCommandBytes(t *testing.T) {
	bin := rubyBin(t)
	// A stubbed Net::IMAP that records put_string instead of touching a socket.
	preamble := `
require "monitor"
class Probe < Net::IMAP
  include MonitorMixin
  def initialize; @tagno=0; @tag_prefix="T"; @buf=+""; @utf8_strings=false; @tagged_responses={}; mon_initialize; end
  def buf; @buf; end
  def reset!; @buf=+""; end
  def put_string(s); @buf << s; end
  def get_tagged_response(tag,*); nil; end
end
P = Probe.new
def emit(cmd, *args); P.reset!; P.send(:send_command, cmd, *args); print P.buf; print "\x00"; end
`
	type tc struct {
		name     string
		rubyArgs string // ruby send_command arg list (after the command)
		build    func(b *Builder) (Command, error)
	}
	cases := []tc{
		{"capability", `"CAPABILITY"`, func(b *Builder) (Command, error) { return b.Capability() }},
		{"noop", `"NOOP"`, func(b *Builder) (Command, error) { return b.Noop() }},
		{"logout", `"LOGOUT"`, func(b *Builder) (Command, error) { return b.Logout() }},
		{"login-plain", `"LOGIN", "joe", "secret"`, func(b *Builder) (Command, error) { return b.Login("joe", "secret") }},
		{"login-space", `"LOGIN", "joe", "pa ss"`, func(b *Builder) (Command, error) { return b.Login("joe", "pa ss") }},
		{"login-quote", `"LOGIN", "u", "a\"b"`, func(b *Builder) (Command, error) { return b.Login("u", `a"b`) }},
		{"select", `"SELECT", "INBOX"`, func(b *Builder) (Command, error) { return b.Select("INBOX") }},
		{"examine", `"EXAMINE", "Sent Items"`, func(b *Builder) (Command, error) { return b.Examine("Sent Items") }},
		{"create", `"CREATE", "foo"`, func(b *Builder) (Command, error) { return b.Create("foo") }},
		{"delete", `"DELETE", "foo"`, func(b *Builder) (Command, error) { return b.Delete("foo") }},
		{"rename", `"RENAME", "a", "b"`, func(b *Builder) (Command, error) { return b.Rename("a", "b") }},
		{"subscribe", `"SUBSCRIBE", "foo"`, func(b *Builder) (Command, error) { return b.Subscribe("foo") }},
		{"list", `"LIST", "", "*"`, func(b *Builder) (Command, error) { return b.List("", "*") }},
		{"lsub", `"LSUB", "", "%"`, func(b *Builder) (Command, error) { return b.Lsub("", "%") }},
		{"status", `"STATUS", "box", [Net::IMAP::RawData.new("MESSAGES"), Net::IMAP::RawData.new("UIDNEXT")]`, func(b *Builder) (Command, error) { return b.Status("box", "MESSAGES", "UIDNEXT") }},
		{"copy", `"COPY", Net::IMAP::RawData.new("1,3:5"), "box"`, func(b *Builder) (Command, error) {
			ss, _ := NewSequenceSet(1, SeqRange{3, 5})
			return b.Copy(ss, "box")
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := c.build(NewBuilder("T"))
			if err != nil {
				t.Fatalf("build: %v", err)
			}
			out := rubyEval(t, bin, preamble+"emit("+c.rubyArgs+")")
			want := strings.TrimSuffix(out, "\x00")
			if got.Bytes != want {
				t.Errorf("command %s:\n got  %q\n want %q (MRI)", c.name, got.Bytes, want)
			}
		})
	}
}

// TestOracleStatusEncoding cross-checks the per-argument string encoding (atom vs
// quoted vs literal) against MRI's send_string_data for a corpus of strings.
func TestOracleStringEncoding(t *testing.T) {
	bin := rubyBin(t)
	preamble := `
require "monitor"
class Probe < Net::IMAP
  include MonitorMixin
  def initialize; @buf=+""; @utf8_strings=false; mon_initialize; end
  def put_string(s); @buf << s; end
  def enc(s); @buf=+""; send(:send_string_data, s); @buf; end
end
P = Probe.new
def e(s); print P.enc(s); print "\x00"; end
`
	// All-inline cases only: a literal (multiline / non-ASCII) string would make
	// MRI block waiting for a continuation request, so those are excluded here.
	strs := []string{"plain", "with space", "", "a\"b", "a\\b", "(paren)", "%glob", "*star", "tab\tchar"}
	var script strings.Builder
	script.WriteString(preamble)
	for _, s := range strs {
		script.WriteString("e(" + rubyStringLiteral(s) + ")\n")
	}
	out := rubyEval(t, bin, script.String())
	parts := strings.Split(out, "\x00")
	for i, s := range strs {
		// MRI's send_string_data writes the inline bytes (or, for a literal, only
		// the "{n}\r\n" prefix and then blocks). Our encodeString emits the same
		// inline prefix; capture exactly what lands before any literal payload.
		var sb strings.Builder
		emit := func(x string) { sb.WriteString(x) }
		startLiteral := func(string) {} // payload is sent after a continuation
		encodeString(s, emit, startLiteral)
		if got := sb.String(); got != parts[i] {
			t.Errorf("encodeString(%q) = %q, want %q (MRI)", s, got, parts[i])
		}
	}
}

// TestOracleResponseParse parses a corpus of canned server responses both here
// and via MRI's ResponseParser, and asserts the structural fields match. This is
// the core differential test for the response grammar (status responses, FETCH
// incl. ENVELOPE / FLAGS / literals, LIST / STATUS / SEARCH / CAPABILITY).
func TestOracleResponseParse(t *testing.T) {
	bin := rubyBin(t)
	// Each response is parsed by MRI; the script prints a small, stable projection
	// of the result that we reproduce from our own parse and compare.
	responses := []string{
		"a001 OK LOGIN completed\r\n",
		"a002 NO [ALERT] System down\r\n",
		"+ Ready\r\n",
		"* 22 EXISTS\r\n",
		"* 0 RECENT\r\n",
		"* 9 EXPUNGE\r\n",
		"* OK [UIDVALIDITY 3857529045] UIDs valid\r\n",
		"* OK [PERMANENTFLAGS (\\Deleted \\Seen \\*)] Limited\r\n",
		"* FLAGS (\\Answered \\Flagged \\Deleted \\Seen \\Draft)\r\n",
		"* LIST (\\Noselect) \"/\" ~/Mail/foo\r\n",
		"* LSUB () \".\" #news.comp.mail.misc\r\n",
		"* STATUS blurdybloop (MESSAGES 231 UIDNEXT 44292)\r\n",
		"* SEARCH 2 84 882\r\n",
		"* SEARCH\r\n",
		"* CAPABILITY IMAP4rev1 STARTTLS AUTH=GSSAPI\r\n",
		"* 12 FETCH (FLAGS (\\Seen) UID 4827313)\r\n",
		"* 23 FETCH (UID 4828442 RFC822.SIZE 44827)\r\n",
		"* 14 FETCH (INTERNALDATE \"17-Jul-1996 02:44:25 -0700\")\r\n",
		"* 12 FETCH (BODY[TEXT] {6}\r\nWorld!)\r\n",
		"* 1 FETCH (RFC822 {4}\r\nbody)\r\n",
		"* 12 FETCH (ENVELOPE (\"Wed, 17 Jul 1996 02:23:25 -0700 (PDT)\" \"subj\" ((\"Terry Gray\" NIL \"gray\" \"cac.washington.edu\")) NIL NIL ((NIL NIL \"imap\" \"cac.washington.edu\")) NIL NIL NIL \"<id@host>\"))\r\n",
	}
	// The MRI projection script: parse each response and print a canonical line.
	script := `
rp = Net::IMAP::ResponseParser.new
def proj(r)
  case r
  when Net::IMAP::TaggedResponse
    "T|#{r.tag}|#{r.name}|#{r.data.text}|#{r.data.code&.name}"
  when Net::IMAP::ContinuationRequest
    "C|#{r.data.text}"
  when Net::IMAP::UntaggedResponse
    d = r.data
    body = case d
      when Integer then d.to_s
      when Array then d.map(&:to_s).join(",")
      when Net::IMAP::ResponseText then "#{d.text}/#{d.code&.name}/#{d.code&.data.inspect}"
      when Net::IMAP::MailboxList then "#{d.attr.map(&:to_s).join(',')}|#{d.delim}|#{d.name}"
      when Net::IMAP::StatusData then "#{d.mailbox}|#{d.attr.map{|k,v| "#{k}=#{v}"}.join(',')}"
      when Net::IMAP::FetchData then "#{d.seqno}|#{d.attr.map{|k,v| "#{k}=#{fmt(v)}"}.join(',')}"
      else d.inspect
    end
    "U|#{r.name}|#{body}"
  end
end
def fmt(v)
  case v
  when Array then "[#{v.map(&:to_s).join(' ')}]"
  when Net::IMAP::Envelope then "ENV(#{v.subject}/#{v.from&.map{|a| a.mailbox}&.join(',')}/#{v.message_id})"
  else v.to_s
  end
end
`
	for _, resp := range responses {
		script += "print proj(rp.parse(" + rubyStringLiteral(resp) + ")); print \"\\x00\"\n"
	}
	out := rubyEval(t, bin, script)
	parts := strings.Split(out, "\x00")
	for i, resp := range responses {
		r, err := ParseResponse(resp)
		if err != nil {
			t.Fatalf("ParseResponse(%q): %v", resp, err)
		}
		got := projectGo(r)
		if got != parts[i] {
			t.Errorf("parse %q:\n go  %q\n MRI %q", resp, got, parts[i])
		}
	}
}

// projectGo mirrors the MRI proj() function so the two can be compared.
func projectGo(r Response) string {
	switch v := r.(type) {
	case *TaggedResponse:
		return "T|" + v.Tag + "|" + v.Name + "|" + v.Data.Text + "|" + codeName(v.Data.Code)
	case *ContinuationRequest:
		return "C|" + v.Data.Text
	case *UntaggedResponse:
		return "U|" + v.Name + "|" + untaggedBody(v)
	}
	return "?"
}

func codeName(c *ResponseCode) string {
	if c == nil {
		return ""
	}
	return c.Name
}

func untaggedBody(v *UntaggedResponse) string {
	switch d := v.Data.(type) {
	case int64:
		return strconv.FormatInt(d, 10)
	case []int64:
		ss := make([]string, len(d))
		for i, n := range d {
			ss[i] = strconv.FormatInt(n, 10)
		}
		return strings.Join(ss, ",")
	case []string:
		return strings.Join(d, ",")
	case []Flag:
		return flagsJoin(d)
	case *ResponseText:
		return d.Text + "/" + codeName(d.Code) + "/" + rubyInspectCodeData(d.Code)
	case *MailboxList:
		return flagsJoin(d.Attr) + "|" + d.Delim + "|" + d.Name
	case *StatusData:
		pairs := make([]string, 0, d.Attr.Len())
		for _, k := range d.Attr.Keys() {
			val, _ := d.Attr.Get(k)
			pairs = append(pairs, k+"="+strconv.FormatInt(val, 10))
		}
		return d.Mailbox + "|" + strings.Join(pairs, ",")
	case *FetchData:
		pairs := make([]string, 0, d.Attr.Len())
		for _, k := range d.Attr.Keys() {
			val, _ := d.Attr.Get(k)
			pairs = append(pairs, k+"="+fmtAttr(val))
		}
		return strconv.FormatInt(d.Seqno, 10) + "|" + strings.Join(pairs, ",")
	}
	return "?"
}

func flagsJoin(fs []Flag) string {
	ss := make([]string, len(fs))
	for i, f := range fs {
		ss[i] = string(f)
	}
	return strings.Join(ss, ",")
}

// rubyInspectCodeData renders a resp-text-code's data the way MRI's Array#inspect
// / Integer#inspect / nil.inspect would, for the small set we emit.
func rubyInspectCodeData(c *ResponseCode) string {
	if c == nil || c.Data == nil {
		return "nil"
	}
	switch d := c.Data.(type) {
	case int64:
		return strconv.FormatInt(d, 10)
	case []Flag:
		ss := make([]string, len(d))
		for i, f := range d {
			ss[i] = ":" + string(f) // Ruby symbol: :Seen, :*
		}
		return "[" + strings.Join(ss, ", ") + "]"
	case []string:
		ss := make([]string, len(d))
		for i, s := range d {
			ss[i] = strconv.Quote(s)
		}
		return "[" + strings.Join(ss, ", ") + "]"
	case string:
		return strconv.Quote(d)
	}
	return "nil"
}

// symInspect renders a flag the way Ruby prints a symbol after the leading ':'.
// MRI prints :* for the wildcard and :Name otherwise.
func symInspect(s string) string { return s }

func fmtAttr(v any) string {
	switch d := v.(type) {
	case []Flag:
		ss := make([]string, len(d))
		for i, f := range d {
			ss[i] = string(f)
		}
		return "[" + strings.Join(ss, " ") + "]"
	case int64:
		return strconv.FormatInt(d, 10)
	case string:
		return d
	case *Envelope:
		from := ""
		if d.From != nil {
			ms := make([]string, len(d.From))
			for i, a := range d.From {
				ms[i] = a.Mailbox
			}
			from = strings.Join(ms, ",")
		}
		return "ENV(" + d.Subject + "/" + from + "/" + d.MessageID + ")"
	}
	return "?"
}

// TestOracleUTF7 cross-checks EncodeUTF7 / DecodeUTF7 against MRI's
// Net::IMAP.encode_utf7 / decode_utf7 for a corpus of mailbox names.
func TestOracleUTF7(t *testing.T) {
	bin := rubyBin(t)
	names := []string{
		"INBOX", "", "Drafts", "Sent Items", "foo & bar", "&", "&&",
		"~peter/mail/台北/日本語", "café", "日本語", "a/b/c", "Ärger",
	}
	var enc, dec strings.Builder
	enc.WriteString("$stdout.binmode\n")
	for _, n := range names {
		enc.WriteString("print Net::IMAP.encode_utf7(" + rubyStringLiteral(n) + "); print \"\\x00\"\n")
	}
	encOut := strings.Split(rubyEval(t, bin, enc.String()), "\x00")
	for i, n := range names {
		if got := EncodeUTF7(n); got != encOut[i] {
			t.Errorf("EncodeUTF7(%q) = %q, want %q (MRI)", n, got, encOut[i])
		}
	}
	// Decode round-trips the MRI-encoded forms.
	for _, n := range names {
		dec.WriteString("print Net::IMAP.decode_utf7(Net::IMAP.encode_utf7(" + rubyStringLiteral(n) + ")); print \"\\x00\"\n")
	}
	decOut := strings.Split(rubyEval(t, bin, "$stdout.binmode\n"+dec.String()), "\x00")
	for i, n := range names {
		if got := DecodeUTF7(EncodeUTF7(n)); got != decOut[i] {
			t.Errorf("DecodeUTF7 roundtrip %q = %q, want %q (MRI)", n, got, decOut[i])
		}
	}
}

// TestOracleSASL cross-checks the SASL response encoders against MRI's
// Net::IMAP::SASL authenticators. The Net::IMAP::SASL.authenticator factory only
// exists in newer net-imap; on an older bundled gem the test self-skips (the
// deterministic golden vectors in helpers_test.go hold parity regardless).
func TestOracleSASL(t *testing.T) {
	bin := rubyBin(t)
	requireSASLFactory(t, bin)
	// Newer net-imap emits "WARNING: <MECH> mechanism is deprecated" for CRAM-MD5
	// and LOGIN via Kernel#warn; silence it so it cannot interleave into the
	// captured bytes (CombinedOutput merges stderr, and on some platforms the
	// warning lands on stdout). $VERBOSE=nil plus a no-op Warning#warn covers both.
	script := `$stdout.binmode
$VERBOSE=nil
module Warning; def warn(*); end; end
require "base64"
a=Net::IMAP::SASL.authenticator("PLAIN","joe","secret"); print Base64.strict_encode64(a.process(nil)); print "\x00"
b=Net::IMAP::SASL.authenticator("XOAUTH2","joe","tok"); print Base64.strict_encode64(b.process(nil)); print "\x00"
c=Net::IMAP::SASL.authenticator("CRAM-MD5","joe","secret"); print c.process("<1234@host>"); print "\x00"
l=Net::IMAP::SASL.authenticator("LOGIN","joe","secret"); print l.process(nil); print "\x00"; print l.process("Password:"); print "\x00"
`
	out := strings.Split(rubyEval(t, bin, script), "\x00")
	if got := SASLEncode(SASLPlain("", "joe", "secret")); got != out[0] {
		t.Errorf("PLAIN = %q, want %q (MRI)", got, out[0])
	}
	if got := SASLEncode(SASLXOAuth2("joe", "tok")); got != out[1] {
		t.Errorf("XOAUTH2 = %q, want %q (MRI)", got, out[1])
	}
	if got := SASLCramMD5("joe", "secret", "<1234@host>"); got != out[2] {
		t.Errorf("CRAM-MD5 = %q, want %q (MRI)", got, out[2])
	}
	if got := SASLLoginUser("joe"); got != out[3] {
		t.Errorf("LOGIN user = %q, want %q (MRI)", got, out[3])
	}
	if got := SASLLoginPassword("secret"); got != out[4] {
		t.Errorf("LOGIN pass = %q, want %q (MRI)", got, out[4])
	}
}

// TestOracleFormatDates cross-checks FormatDate / FormatDatetime against MRI's
// Net::IMAP.format_date / format_datetime.
func TestOracleFormatDates(t *testing.T) {
	bin := rubyBin(t)
	script := `$stdout.binmode
t=Time.utc(2026,6,30,12,5,9)
print Net::IMAP.format_date(t); print "\x00"
print Net::IMAP.format_datetime(t); print "\x00"
`
	out := strings.Split(rubyEval(t, bin, script), "\x00")
	tm := time.Date(2026, time.June, 30, 12, 5, 9, 0, time.UTC)
	if got := FormatDate(tm); got != out[0] {
		t.Errorf("FormatDate = %q, want %q (MRI)", got, out[0])
	}
	if got := FormatDatetime(tm); got != out[1] {
		t.Errorf("FormatDatetime = %q, want %q (MRI)", got, out[1])
	}
}

// TestOracleNewCommands cross-checks the EXPUNGE/CHECK/CLOSE/UNSELECT/IDLE/
// AUTHENTICATE builders against MRI's send_command byte output.
func TestOracleNewCommands(t *testing.T) {
	bin := rubyBin(t)
	preamble := `
require "monitor"
class Probe < Net::IMAP
  include MonitorMixin
  def initialize; @tagno=0; @tag_prefix="T"; @buf=+""; @utf8_strings=false; @tagged_responses={}; mon_initialize; end
  def buf; @buf; end
  def reset!; @buf=+""; end
  def put_string(s); @buf << s; end
  def get_tagged_response(tag,*); nil; end
end
P = Probe.new
def emit(cmd, *args); P.reset!; P.send(:send_command, cmd, *args); print P.buf; print "\x00"; end
`
	cases := []struct {
		ruby  string
		build func(b *Builder) (Command, error)
	}{
		{`"EXPUNGE"`, func(b *Builder) (Command, error) { return b.Expunge() }},
		{`"CHECK"`, func(b *Builder) (Command, error) { return b.Check() }},
		{`"CLOSE"`, func(b *Builder) (Command, error) { return b.Close() }},
		{`"UNSELECT"`, func(b *Builder) (Command, error) { return b.Unselect() }},
		{`"IDLE"`, func(b *Builder) (Command, error) { return b.Idle() }},
		{`"AUTHENTICATE", Net::IMAP::RawData.new("CRAM-MD5")`, func(b *Builder) (Command, error) { return b.Authenticate("CRAM-MD5") }},
	}
	for _, c := range cases {
		got, err := c.build(NewBuilder("T"))
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		want := strings.TrimSuffix(rubyEval(t, bin, preamble+"emit("+c.ruby+")"), "\x00")
		if got.Bytes != want {
			t.Errorf("got %q want %q (MRI)", got.Bytes, want)
		}
	}
}

// rubyStringLiteral renders s as a Ruby double-quoted string literal so a Go
// string round-trips exactly into the Ruby script (CRLF, quotes, backslashes).
func rubyStringLiteral(s string) string {
	var sb strings.Builder
	sb.WriteByte('"')
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '\\':
			sb.WriteString(`\\`)
		case '"':
			sb.WriteString(`\"`)
		case '\r':
			sb.WriteString(`\r`)
		case '\n':
			sb.WriteString(`\n`)
		case '#':
			sb.WriteString(`\#`) // avoid Ruby #{} interpolation
		default:
			if c < 0x20 {
				sb.WriteString(`\x`)
				const hex = "0123456789ABCDEF"
				sb.WriteByte(hex[c>>4])
				sb.WriteByte(hex[c&0xf])
			} else {
				sb.WriteByte(c)
			}
		}
	}
	sb.WriteByte('"')
	return sb.String()
}
