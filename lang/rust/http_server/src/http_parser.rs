use Default;
use std::ops::Range;

const DEFAULT_HEADER_NAME_LEN: usize = 32;

/// 解析中的状态机。解析期字段"还没有"是常态,内部用 Option 是诚实的;
/// 解析完成时通过 complete() 产出字段全为具体类型的 Request。
#[derive(Debug, PartialEq, Default)]
pub struct Parser {
    state: ParseState,
    buffer: Vec<u8>,
    // 当前解析到 buffer 的位置
    // 如果解析完成了，cursor 就是 buffer.len()
    cursor: usize,
    method: Option<HttpMethod>,
    // [start, end) 的形式,方便切片
    request_method_end: Option<usize>,
    request_path_start: Option<usize>,
    request_path_end: Option<usize>,
    request_http_version_major_start: Option<usize>,
    request_http_version_major_end: Option<usize>,
    request_http_version_minor_start: Option<usize>,
    request_http_version_minor_end: Option<usize>,
    request_heaeder_name_start: Option<usize>,
    request_current_lowercase_header_name: Option<Vec<u8>>,
    request_heaeder_name_end: Option<usize>,
    request_heaeder_value_start: Option<usize>,
    request_heaeders_raw: Option<Vec<(Range<usize>, Range<usize>)>>,
    request_host: Option<Range<usize>>,
    request_content_length_start: Option<usize>,
    request_port: Option<Range<usize>>,
    request_content_length: Option<u32>,
    request_body_start: Option<usize>,
}

/// 解析完成的产物:所有字段必然存在,没有 Option。
#[derive(Debug, PartialEq)]
pub struct Request<'a> {
    method: HttpMethod,
    method_bytes: &'a [u8],
    path: &'a [u8],
    http_version_major: &'a [u8],
    http_version_minor: &'a [u8],
    headers: Vec<(&'a [u8], &'a [u8])>,
    body: Option<&'a [u8]>,
    buffer: &'a [u8],
}

impl<'a> Request<'a> {
    pub fn method(&self) -> &HttpMethod {
        &self.method
    }

    pub fn path(&self) -> &[u8] {
        self.path
    }

    pub fn http_version_major(&self) -> &[u8] {
        self.http_version_major
    }

    pub fn http_version_minor(&self) -> &[u8] {
        self.http_version_minor
    }

    pub fn headers(&self) -> &Vec<(&[u8], &[u8])> {
        &self.headers
    }

    pub fn body(&self) -> Option<&[u8]> {
        self.body
    }

    pub fn buffer(&self) -> &[u8] {
        self.buffer
    }
}

#[derive(Debug, PartialEq)]
pub struct Response {
    pub status_code: u16,
    pub headers: Vec<(String, String)>,
    pub body: Option<String>,
}

#[derive(Debug, PartialEq)]
pub enum ParseStatus<'a> {
    Complete(Request<'a>),
    Partial,
}

#[derive(Debug, PartialEq, Default)]
pub enum ParseState {
    #[default]
    RequestLineStart,
    RequestMethod,
    RequestLineSpaceBeforeUrl,
    RequestAfterSlashInUri,
    RequestTargetCheckUri,
    RequestTargetSchema,
    RequestTargetHost,
    RequestH,
    RequestHT,
    RequestHTT,
    RequestHTTP,
    RequestHTTPSlash,
    RequestHTTPVersionMajor,
    RequestHTTPVersionMinor,
    RequestNBeforeHeader,
    RequestHeaderName,
    RequestHeaderComma,
    RequestSpaceBeforeHeaderValue,
    RequestHeaderValue,
    RequestRAfterHeaderValue,
    RequestNAfterHeaderValue,
    RequestRBeforeBody,
    RequestBody,
}
#[derive(Debug, PartialEq)]
pub enum ParseErr {
    InvalidMethod,
    InvalidHttpVersion,
    InvalidRequest,
}

#[derive(Debug, PartialEq)]
pub enum HttpMethod {
    Post,
    Put,
    Get,
    Trace,
    Head,
    Delete,
    Options,
    Connect,
}

impl Parser {
    pub fn new(data: Vec<u8>) -> Self {
        Parser {
            buffer: data,
            ..Default::default()
        }
    }

    fn cur(&mut self, buffer: &[u8]) -> Option<u8> {
        if self.cursor >= buffer.len() {
            return None;
        }
        Some(buffer[self.cursor])
    }

    fn peek(&mut self, n: usize) -> Option<u8> {
        if self.cursor + n >= self.buffer.len() {
            return None;
        }
        Some(self.buffer[self.cursor + n])
    }

    // `'a` is load-bearing: the returned Request borrows the BYTES, not the parser.
    // Without it, elision ties the output to `&mut self` and the parser stays
    // mutably borrowed for as long as any Request is alive.
    pub fn parse<'a>(&mut self, buffer: &'a [u8]) -> Result<ParseStatus<'a>, ParseErr> {
        while let Some(c) = self.cur(buffer) {
            match self.state {
                ParseState::RequestLineStart => match c {
                    b'P' | b'G' | b'T' | b'H' | b'D' | b'O' | b'C' => {
                        self.state = ParseState::RequestMethod;
                        continue;
                    }
                    _ => return Err(ParseErr::InvalidMethod),
                },
                ParseState::RequestMethod => {
                    if c == b' ' {
                        self.state = ParseState::RequestLineSpaceBeforeUrl;
                        self.request_method_end = Some(self.cursor);
                        match &buffer[..self.cursor] {
                            b"POST" => {
                                self.method = Some(HttpMethod::Post);
                            }
                            b"PUT" => {
                                self.method = Some(HttpMethod::Put);
                            }
                            b"GET" => {
                                self.method = Some(HttpMethod::Get);
                            }
                            b"TRACE" => {
                                self.method = Some(HttpMethod::Trace);
                            }
                            b"HEAD" => {
                                self.method = Some(HttpMethod::Head);
                            }
                            b"DELETE" => {
                                self.method = Some(HttpMethod::Delete);
                            }
                            b"OPTIONS" => {
                                self.method = Some(HttpMethod::Options);
                            }
                            b"CONNECT" => {
                                self.method = Some(HttpMethod::Connect);
                            }
                            _ => {
                                return Err(ParseErr::InvalidMethod);
                            }
                        }
                        self.cursor += 1;
                        continue;
                    }
                    if c < b'A' || c > b'Z' {
                        return Err(ParseErr::InvalidMethod);
                    }
                    self.cursor += 1;
                    continue;
                }
                ParseState::RequestLineSpaceBeforeUrl => {
                    if c == b'/' {
                        self.request_path_start = Some(self.cursor);
                        self.cursor += 1;
                        self.state = ParseState::RequestAfterSlashInUri;
                        continue;
                    }
                }
                ParseState::RequestAfterSlashInUri => {
                    if is_usual(c) {
                        self.state = ParseState::RequestTargetCheckUri;
                        continue;
                    }
                    // multiple `//`
                    if c == b'/' {
                        todo!()
                    }
                }
                ParseState::RequestTargetSchema => todo!(),
                ParseState::RequestTargetHost => todo!(),
                ParseState::RequestTargetCheckUri => {
                    if is_usual(c) {
                        self.cursor += 1;
                        continue;
                    }
                    match c {
                        b'/' => {
                            self.cursor += 1;
                            self.state = ParseState::RequestAfterSlashInUri;
                        }
                        b' ' => {
                            self.request_path_end = Some(self.cursor);
                            self.state = ParseState::RequestH;
                            self.cursor += 1;
                        }
                        _ => todo!(),
                    }
                }
                ParseState::RequestH => match c {
                    b'H' => {
                        self.cursor += 1;
                        self.state = ParseState::RequestHT;
                    }
                    _ => return Err(ParseErr::InvalidHttpVersion),
                },
                ParseState::RequestHT => match c {
                    b'T' => {
                        self.cursor += 1;
                        self.state = ParseState::RequestHTT;
                    }
                    _ => return Err(ParseErr::InvalidHttpVersion),
                },
                ParseState::RequestHTT => match c {
                    b'T' => {
                        self.cursor += 1;
                        self.state = ParseState::RequestHTTP;
                    }
                    _ => return Err(ParseErr::InvalidHttpVersion),
                },
                ParseState::RequestHTTP => match c {
                    b'P' => {
                        self.cursor += 1;
                        self.state = ParseState::RequestHTTPSlash;
                    }
                    _ => return Err(ParseErr::InvalidHttpVersion),
                },
                ParseState::RequestHTTPSlash => match c {
                    b'/' => {
                        self.cursor += 1;
                        self.request_http_version_major_start = Some(self.cursor);
                        self.state = ParseState::RequestHTTPVersionMajor;
                    }
                    _ => return Err(ParseErr::InvalidHttpVersion),
                },
                ParseState::RequestHTTPVersionMajor => {
                    if c >= b'0' && c <= b'9' {
                        self.cursor += 1;
                        continue;
                    }
                    if c == b'.' {
                        self.request_http_version_major_end = Some(self.cursor);
                        self.cursor += 1;
                        self.request_http_version_minor_start = Some(self.cursor);
                        self.state = ParseState::RequestHTTPVersionMinor;
                        continue;
                    }
                    return Err(ParseErr::InvalidHttpVersion);
                }
                ParseState::RequestHTTPVersionMinor => {
                    if c >= b'0' && c <= b'9' {
                        self.cursor += 1;
                        continue;
                    }
                    if c == b'\r' {
                        self.request_http_version_minor_end = Some(self.cursor);
                        self.cursor += 1;
                        self.state = ParseState::RequestNBeforeHeader;
                        continue;
                    }
                    return Err(ParseErr::InvalidHttpVersion);
                }
                ParseState::RequestNBeforeHeader => {
                    if c == b'\n' {
                        self.cursor += 1;
                        self.state = ParseState::RequestHeaderName;
                        self.request_heaeder_name_start = Some(self.cursor);
                        continue;
                    }
                    return Err(ParseErr::InvalidRequest);
                }
                ParseState::RequestHeaderName => {
                    if valid_header_name(c) {
                        let name = self
                            .request_current_lowercase_header_name
                            .get_or_insert(Vec::with_capacity(DEFAULT_HEADER_NAME_LEN));
                        let start = self
                            .request_heaeder_name_start
                            .expect("header name start must be set");
                        name.push(HEADER_NAME_LOWCASE[c as usize]);
                        self.cursor += 1;
                        continue;
                    }
                    if c == b':' {
                        self.request_heaeder_name_end = Some(self.cursor);
                        self.cursor += 1;
                        self.state = ParseState::RequestHeaderComma;
                        continue;
                    }
                    return Err(ParseErr::InvalidRequest);
                }
                ParseState::RequestHeaderComma => {
                    if c == b' ' {
                        self.cursor += 1;
                        self.request_heaeder_value_start = Some(self.cursor);
                        self.state = ParseState::RequestSpaceBeforeHeaderValue;
                        continue;
                    }
                    if c == b'\r' || c == b'\n' {
                        return Err(ParseErr::InvalidRequest);
                    }
                    self.request_heaeder_value_start = Some(self.cursor);
                    self.cursor += 1;
                    self.state = ParseState::RequestHeaderValue;
                }
                ParseState::RequestSpaceBeforeHeaderValue => {
                    if c == b'\r' || c == b'\n' {
                        return Err(ParseErr::InvalidRequest);
                    }
                    self.request_heaeder_value_start = Some(self.cursor);
                    self.cursor += 1;
                    self.state = ParseState::RequestHeaderValue;
                }
                ParseState::RequestHeaderValue => {
                    if c == b'\r' {
                        let request_headers_raw =
                            self.request_heaeders_raw.get_or_insert(Vec::new());
                        let name_start = self
                            .request_heaeder_name_start
                            .expect("header name start must be set");
                        let name_end = self
                            .request_heaeder_name_end
                            .expect("header name end must be set");
                        let value_start = self
                            .request_heaeder_value_start
                            .expect("header value start must be set");
                        request_headers_raw.push((name_start..name_end, value_start..self.cursor));
                        let lowercase_name = self
                            .request_current_lowercase_header_name
                            .take()
                            .expect("header name must be set");
                        match lowercase_name.as_slice() {
                            b"host" => {
                                self.request_host =
                                    Some(self.request_heaeder_value_start.unwrap()..self.cursor);
                            }
                            b"content-length" => {
                                let content_length_str = std::str::from_utf8(
                                    &buffer[self.request_heaeder_value_start.unwrap()..self.cursor],
                                )
                                .map_err(|_| ParseErr::InvalidRequest)?;
                                let content_length = content_length_str
                                    .parse::<u32>()
                                    .map_err(|_| ParseErr::InvalidRequest)?;
                                self.request_content_length = Some(content_length);
                            }
                            _ => {}
                        }
                        self.cursor += 1;
                        self.state = ParseState::RequestRAfterHeaderValue;
                        continue;
                    }
                    if c == b'\n' {
                        return Err(ParseErr::InvalidRequest);
                    }
                    self.cursor += 1;
                }
                ParseState::RequestRAfterHeaderValue => {
                    if c == b'\n' {
                        self.cursor += 1;
                        self.state = ParseState::RequestNAfterHeaderValue;
                        continue;
                    }
                    return Err(ParseErr::InvalidRequest);
                }
                ParseState::RequestNAfterHeaderValue => {
                    if valid_header_name(c) {
                        self.request_heaeder_name_start = Some(self.cursor);
                        self.state = ParseState::RequestHeaderName;
                        continue;
                    }
                    if c == b'\r' {
                        self.cursor += 1;
                        self.state = ParseState::RequestRBeforeBody;
                        continue;
                    }
                    return Err(ParseErr::InvalidRequest);
                }
                ParseState::RequestRBeforeBody => {
                    if c == b'\n' {
                        if let Some(content_length) = self.request_content_length {
                            self.cursor += 1;
                            self.request_body_start = Some(self.cursor);
                            self.state = ParseState::RequestBody;
                            continue;
                        } else {
                            // TODO: handle chunked transfer encoding
                            self.cursor += 1; // consume the '\n' — the message ends after it
                            return Ok(ParseStatus::Complete(self.complete(buffer)));
                        }
                    }
                    return Err(ParseErr::InvalidRequest);
                }
                ParseState::RequestBody => {
                    let content_length = self
                        .request_content_length
                        .expect("content length must been set before")
                        as usize;
                    let start = self
                        .request_body_start
                        .expect("body start must been set before");
                    self.cursor += 1;
                    if self.cursor - start < content_length {
                        continue;
                    } else if self.cursor - start == content_length {
                        return Ok(ParseStatus::Complete(self.complete(buffer)));
                    }
                }
            }
        }
        Ok(ParseStatus::Partial)
    }

    /// 请求行终结时调用(终结逻辑由你在状态机里接上),把 Parser 转化为 Request。
    /// unwrap 集中在这一处,由状态机不变量背书:能走到请求行结束,
    /// method 和 target 的 start/end 必然都已赋值。
    #[allow(dead_code)]
    fn complete<'a>(&mut self, buffer: &'a [u8]) -> Request<'a> {
        let method = self
            .method
            .take()
            .expect("method is set before request line completes");
        let method_end = self
            .request_method_end
            .expect("method end is set before request line completes");
        let start = self
            .request_path_start
            .expect("target start is set before request line completes");
        let end = self
            .request_path_end
            .expect("target end is set before request line completes");
        let http_version_major_start = self
            .request_http_version_major_start
            .expect("http version major start is set before request line completes");
        let http_version_major_end = self
            .request_http_version_major_end
            .expect("http version major end is set before request line completes");
        let http_version_minor_start = self
            .request_http_version_minor_start
            .expect("http version minor start is set before request line completes");
        let http_version_minor_end = self
            .request_http_version_minor_end
            .expect("http version minor end is set before request line completes");
        let mut headers = Vec::new();
        for (name_range, value_range) in self.request_heaeders_raw.get_or_insert(Vec::new()) {
            let name = &buffer[name_range.clone()];
            let value = &buffer[value_range.clone()];
            headers.push((name, value));
        }
        let body = if let Some(content_length) = self.request_content_length {
            let start = self
                .request_body_start
                .expect("body start is set before request line completes");
            let end = start + content_length as usize;
            Some(&buffer[start..end])
        } else {
            None
        };
        Request {
            method,
            method_bytes: &buffer[..method_end],
            buffer: &buffer[..self.cursor],
            path: &buffer[start..end],
            http_version_major: &buffer[http_version_major_start..http_version_major_end],
            http_version_minor: &buffer[http_version_minor_start..http_version_minor_end],
            headers,
            body,
        }
    }
}

const USUAL: [u32; 8] = [
    /* 0000 0000 0000 0000  0000 0000 0000 0000 */
    0x00000000,
    /* ?>=< ;:98 7654 3210  /.-, +*)( '&%$ #"!  */
    /* 0111 1111 1111 1111  0011 0111 1101 0110 */
    0x7fff37d6,
    /* _^]\ [ZYX WVUT SRQP  ONML KJIH GFED CBA@ */
    /* 1111 1111 1111 1111  1111 1111 1111 1111 */
    0xffffffff,
    /*  ~}| {zyx wvut srqp  onml kjih gfed cba` */
    /* 0111 1111 1111 1111  1111 1111 1111 1111 */
    0x7fffffff, 0xffffffff, /* 1111 1111 1111 1111  1111 1111 1111 1111 */
    0xffffffff, /* 1111 1111 1111 1111  1111 1111 1111 1111 */
    0xffffffff, /* 1111 1111 1111 1111  1111 1111 1111 1111 */
    0xffffffff, /* 1111 1111 1111 1111  1111 1111 1111 1111 */
];

fn is_usual(c: u8) -> bool {
    return USUAL[(c >> 5) as usize] & (1 << (c & 0x01f)) != 0;
}

const HEADER_NAME_LOWCASE: [u8; 256] = {
    let mut t = [0u8; 256];
    let mut c = b'a';
    while c <= b'z' {
        t[c as usize] = c;
        c += 1;
    }
    let mut c = b'A';
    while c <= b'Z' {
        t[c as usize] = c - b'A' + b'a'; // 大写 → 小写
        c += 1;
    }
    let mut c = b'0';
    while c <= b'9' {
        t[c as usize] = c;
        c += 1;
    }
    t[b'-' as usize] = b'-';
    t
};

fn valid_header_name(c: u8) -> bool {
    HEADER_NAME_LOWCASE[c as usize] != 0
}

#[cfg(test)]
mod tests {
    use super::*;

    const FULL_POST: &[u8] =
        b"POST /a/b/c HTTP/1.1\r\nHost: test.com\r\nContent-Length: 1\r\n\r\nh";

    // ---- partial (resume-state) tests: freeze the machine mid-token,
    //      assert cursor/state/marks — these are what catch resume bugs ----

    #[test]
    fn partial_mid_method() {
        let mut p = Parser::default();
        assert_eq!(p.parse(b"PO"), Ok(ParseStatus::Partial));
        assert_eq!(
            p,
            Parser {
                state: ParseState::RequestMethod,
                cursor: 2,
                ..Default::default()
            }
        );
    }

    #[test]
    fn partial_method_undecided_until_space() {
        let mut p = Parser::default();
        assert_eq!(p.parse(b"POST"), Ok(ParseStatus::Partial));
        assert_eq!(
            p,
            Parser {
                state: ParseState::RequestMethod,
                cursor: 4,
                ..Default::default()
            }
        );
    }

    #[test]
    fn partial_after_method_space() {
        let mut p = Parser::default();
        assert_eq!(p.parse(b"POST "), Ok(ParseStatus::Partial));
        assert_eq!(
            p,
            Parser {
                method: Some(HttpMethod::Post),
                state: ParseState::RequestLineSpaceBeforeUrl,
                cursor: 5,
                request_method_end: Some(4),
                ..Default::default()
            }
        );
    }

    #[test]
    fn partial_after_path() {
        let mut p = Parser::default();
        assert_eq!(p.parse(b"POST /a/b/c "), Ok(ParseStatus::Partial));
        assert_eq!(
            p,
            Parser {
                method: Some(HttpMethod::Post),
                state: ParseState::RequestH,
                cursor: 12,
                request_method_end: Some(4),
                request_path_start: Some(5),
                request_path_end: Some(11),
                ..Default::default()
            }
        );
    }

    #[test]
    fn partial_mid_http_token() {
        let mut p = Parser::default();
        assert_eq!(p.parse(b"POST /a/b/c H"), Ok(ParseStatus::Partial));
        assert_eq!(p.state, ParseState::RequestHT);
        assert_eq!(p.cursor, 13);
    }

    #[test]
    fn partial_before_version_slash() {
        let mut p = Parser::default();
        assert_eq!(p.parse(b"POST /a/b/c HTTP"), Ok(ParseStatus::Partial));
        assert_eq!(p.state, ParseState::RequestHTTPSlash);
        assert_eq!(p.cursor, 16);
    }

    #[test]
    fn partial_after_version_dot() {
        let mut p = Parser::default();
        assert_eq!(p.parse(b"POST /a/b/c HTTP/1."), Ok(ParseStatus::Partial));
        assert_eq!(
            p,
            Parser {
                method: Some(HttpMethod::Post),
                state: ParseState::RequestHTTPVersionMinor,
                cursor: 19,
                request_method_end: Some(4),
                request_path_start: Some(5),
                request_path_end: Some(11),
                request_http_version_major_start: Some(17),
                request_http_version_major_end: Some(18),
                request_http_version_minor_start: Some(19), // eager: points past buffer end
                ..Default::default()
            }
        );
    }

    // ---- complete parses ----

    #[test]
    fn complete_post_with_body() {
        let mut p = Parser::default();
        assert_eq!(
            p.parse(FULL_POST),
            Ok(ParseStatus::Complete(Request {
                method: HttpMethod::Post,
                method_bytes: b"POST",
                path: b"/a/b/c",
                http_version_major: b"1",
                http_version_minor: b"1",
                headers: vec![
                    (b"Host".as_slice(), b"test.com".as_slice()),
                    (b"Content-Length".as_slice(), b"1".as_slice()),
                ],
                body: Some(b"h".as_slice()),
                buffer: FULL_POST,
            }))
        );
    }

    #[test]
    fn complete_request_views_point_into_input() {
        let mut p = Parser::default();
        match p.parse(FULL_POST) {
            Ok(ParseStatus::Complete(req)) => {
                assert_eq!(req.path, b"/a/b/c");
                assert_eq!(req.body, Some(b"h".as_slice()));
                assert_eq!(req.headers[0], (b"Host".as_slice(), b"test.com".as_slice()));
                // zero-copy proof: the view has the SAME address as the input, not a copy
                assert_eq!(req.path.as_ptr(), FULL_POST[5..].as_ptr());
                assert_eq!(req.buffer.len(), FULL_POST.len()); // buffer == consumed bytes
            }
            other => panic!("expected Complete, got {other:?}"),
        }
    }

    #[test]
    fn complete_get_bodyless() {
        let input = b"GET /a/b/c HTTP/1.1\r\nHost: test.com\r\n\r\n";
        let mut p = Parser::default();
        assert_eq!(
            p.parse(input),
            Ok(ParseStatus::Complete(Request {
                method: HttpMethod::Get,
                method_bytes: b"GET",
                path: b"/a/b/c",
                http_version_major: b"1",
                http_version_minor: b"1",
                headers: vec![(b"Host".as_slice(), b"test.com".as_slice())],
                body: None,
                buffer: input,
            }))
        );
    }

    #[test]
    fn header_section_unterminated_is_partial() {
        // one \r\n short of the blank line: must NOT complete
        let mut p = Parser::default();
        assert_eq!(
            p.parse(b"GET /a/b/c HTTP/1.1\r\nHost: test.com\r\n"),
            Ok(ParseStatus::Partial)
        );
        assert_eq!(p.state, ParseState::RequestNAfterHeaderValue);
    }

    // ---- incremental parsing: same bytes, delivered in pieces ----

    #[test]
    fn incremental_across_segments() {
        let mut p = Parser::default();
        let mut buf: Vec<u8> = Vec::new();

        buf.extend_from_slice(b"POST /a");
        assert_eq!(p.parse(&buf), Ok(ParseStatus::Partial));

        buf.extend_from_slice(b"/b/c HTTP/1.");
        assert_eq!(p.parse(&buf), Ok(ParseStatus::Partial));
        assert_eq!(p.state, ParseState::RequestHTTPVersionMinor);
        assert_eq!(p.cursor, 19);

        buf.extend_from_slice(b"1\r\nHost: test.com\r\nContent-Length: 1\r\n\r\nh");
        match p.parse(&buf) {
            Ok(ParseStatus::Complete(req)) => {
                assert_eq!(req.method, HttpMethod::Post);
                assert_eq!(req.path, b"/a/b/c");
                assert_eq!(req.body, Some(b"h".as_slice()));
            }
            other => panic!("expected Complete, got {other:?}"),
        }
    }

    #[test]
    fn byte_at_a_time() {
        // torture test: every possible split point at once.
        // catches any state that can't resume across a feed boundary.
        let mut p = Parser::default();
        let mut completed = None;
        for end in 1..=FULL_POST.len() {
            match p.parse(&FULL_POST[..end]).expect("no parse error") {
                ParseStatus::Complete(req) => {
                    completed = Some((req, end));
                    break;
                }
                ParseStatus::Partial => {}
            }
        }
        let (req, at) = completed.expect("should complete once all bytes arrived");
        assert_eq!(
            at,
            FULL_POST.len(),
            "completed exactly at the last body byte"
        );
        assert_eq!(req.path, b"/a/b/c");
        assert_eq!(req.body, Some(b"h".as_slice()));
    }

    // ---- errors ----

    #[test]
    fn invalid_method_first_byte() {
        let mut p = Parser::default();
        assert_eq!(p.parse(b"X / HTTP/1.1\r\n"), Err(ParseErr::InvalidMethod));
    }

    #[test]
    fn invalid_http_version() {
        let mut p = Parser::default();
        assert_eq!(
            p.parse(b"POST /a HTTP/x.1\r\n"),
            Err(ParseErr::InvalidHttpVersion)
        );
    }

    #[test]
    fn invalid_content_length_value() {
        let mut p = Parser::default();
        assert_eq!(
            p.parse(b"POST /a HTTP/1.1\r\nContent-Length: abc\r\n\r\n"),
            Err(ParseErr::InvalidRequest)
        );
    }
}
