use crate::{
    event::{
        Cmd, EventCtx, EventDriver, EventHandler, SignalHandler, TimerCallback, UnixSignal, read,
        write,
    },
    http_parser::{ParseStatus, Parser, Request, Response},
    promise::Promise,
};
use std::{
    cell::RefCell,
    collections::{HashMap, VecDeque},
    io::Error,
    net::SocketAddr,
    os::fd::OwnedFd,
    rc::Rc, sync::atomic::AtomicU32,
};

type OnRequestCallback = Box<
    dyn Fn(
        &mut HttpContext,
        &mut Request,
        &mut HttpResponseWriter,
    ) -> Promise<HttpResponseWriter, String>,
>;
type OnLogCallback = Box<dyn Fn(&mut Request, HttpResponseWriter)>;

#[derive(Debug)]
pub struct HttpResponseWriter {
    pub status_code: u16,
    pub headers: HashMap<String, String>,
    pub body: String,
}

impl HttpResponseWriter {
    pub fn endcode(&mut self) -> Vec<u8> {
        let mut response = format!("HTTP/1.1 {} OK\r\n", self.status_code);
        self.headers
            .insert("Content-Length".to_string(), self.body.len().to_string());
        for (key, value) in &self.headers {
            response.push_str(&format!("{}: {}\r\n", key, value));
        }
        response.push_str("\r\n");
        response.push_str(&self.body);
        response.into_bytes()
    }
}
pub struct HttpConnection {
    shuting_down: bool,
    inflight_requests: u32,
    fd: OwnedFd,
    parser: Parser,
    // the connection owns the byte stream; the parser only keeps positions into it
    read_buf: Vec<u8>,
    on_request: Option<OnRequestCallback>,
    on_log: Option<OnLogCallback>,

    write_queue: Rc<RefCell<VecDeque<Vec<u8>>>>,
    write_offset: usize,
}

pub struct HttpContext<'http> {
    ctx: &'http mut crate::event::EventCtx,
}

impl<'http> HttpContext<'http> {
    pub fn new(ctx: &'http mut crate::event::EventCtx) -> Self {
        Self { ctx }
    }

    pub fn set_timeout(&mut self, duration: std::time::Duration, callback: TimerCallback) {
        self.ctx.add_timer(duration, callback);
    }
}

impl HttpConnection {
    pub fn new(fd: OwnedFd) -> Self {
        Self {
            shuting_down: false,
            inflight_requests: 0,
            fd,
            parser: Parser::default(),
            read_buf: Vec::new(),
            on_request: None,
            on_log: Some(Box::new(|request, response| {
                log::debug!("Request: {:?}, Response: {:?}", request, response);
            })),
            write_queue: Rc::new(RefCell::new(VecDeque::new())),
            write_offset: 0,
        }
    }
    pub fn on_request(&mut self, callback: OnRequestCallback) {
        self.on_request = Some(callback);
    }
    pub fn on_log(&mut self, callback: OnLogCallback) {
        self.on_log = Some(callback);
    }

    pub fn on_readable(&mut self, ctx: &mut EventCtx) {
        // 1. drain the socket into read_buf (edge-triggered: must read to EAGAIN)
        let mut buf = [0u8; 1024];
        loop {
            match read(&self.fd, &mut buf) {
                Ok(0) => {
                    // peer closed
                    ctx.close();
                    return;
                }
                Ok(n) => self.read_buf.extend_from_slice(&buf[..n]),
                Err(e) if e.raw_os_error() == Some(libc::EAGAIN) => break,
                Err(e) => {
                    log::debug!("read error, closing: {e}");
                    ctx.close();
                    return;
                }
            }
        }

        // 2. drain complete messages from read_buf (loop = pipelining support)
        loop {
            match self.parser.parse(&self.read_buf) {
                Ok(ParseStatus::Complete(mut request)) => {
                    let consumed = request.buffer().len(); // Request owns exactly its own bytes
                    let mut response = HttpResponseWriter {
                        status_code: 200,
                        headers: HashMap::new(),
                        body: String::new(),
                    };
                    if let Some(ref callback) = self.on_request {
                        self.inflight_requests += 1;
                        let mut http_ctx = HttpContext::new(ctx);
                        let promise = callback(&mut http_ctx, &mut request, &mut response);
                        let write_queue = self.write_queue.clone();
                        let sender = ctx.sender();
                        promise.then(Box::new(move |mut response| {
                            let data = response.endcode();
                            write_queue.borrow_mut().push_back(data);
                            sender.enable_write();
                        }));
                    }
                    if let Some(ref callback) = self.on_log {
                        callback(&mut request, response);
                    }
                    self.read_buf.drain(..consumed);
                    self.parser = Parser::default(); // fresh state machine for the next message
                }
                Ok(ParseStatus::Partial) => break, // need more bytes from the wire
                Err(e) => {
                    log::warn!("parse error, closing: {e:?}");
                    ctx.close();
                    return;
                }
            }
        }
    }

    pub fn on_writable(&mut self, ctx: &mut EventCtx) {
        let mut write_queue = self.write_queue.borrow_mut();
        while let Some(data) = write_queue.front() {
            let new_offset = self.flush(data, self.write_offset, ctx);
            if new_offset == data.len() {
                write_queue.pop_front();
                self.inflight_requests -= 1;
                self.write_offset = 0;
            } else {
                self.write_offset = new_offset;
                break;
            }
        }
        if write_queue.is_empty() {
            ctx.disable_write();
            if self.inflight_requests == 0 && self.shuting_down {
                ctx.close();
            }
        }
    }

    pub fn flush(&self, data: &Vec<u8>, mut offset: usize, ctx: &mut EventCtx) -> usize {
        while offset < data.len() {
            let data_to_write = &data[offset..];
            match write(&self.fd, data_to_write) {
                Ok(0) => {
                    ctx.close();
                    break;
                }
                Ok(n) => {
                    offset += n;
                }
                Err(e) if e.raw_os_error() == Some(libc::EAGAIN) => break,
                Err(e) => {
                    log::debug!("write error, closing: {e}");
                    ctx.close();
                    break;
                }
            }
        }
        offset
    }
}

impl EventHandler for HttpConnection {
    fn fd(&self) -> &std::os::unix::prelude::OwnedFd {
        &self.fd
    }

    fn on_ready(&mut self, ctx: &mut EventCtx) {
        if ctx.readable() {
            self.on_readable(ctx);
        }
        if ctx.writable() {
            self.on_writable(ctx);
        }
    }
    fn on_shutdown(&mut self, ctx: &mut EventCtx) {
        self.shuting_down = true;
    }
}

pub fn new_http_connection(fd: OwnedFd) -> Box<dyn EventHandler> {
    Box::new(HttpConnection::new(fd))
}

pub type AsyncHttpHandler = Box<
    dyn Fn(
        &mut HttpContext,
        &mut Request,
        &mut HttpResponseWriter,
    ) -> Promise<HttpResponseWriter, String>,
>;

pub type HttpHandler = Box<dyn Fn(&mut HttpContext, &mut Request, &mut HttpResponseWriter)>;

pub struct HttpServer {
    async_handlers: HashMap<Vec<u8>, AsyncHttpHandler>,
    handlers: HashMap<Vec<u8>, HttpHandler>,
}

impl HttpServer {
    pub fn new() -> Self {
        Self {
            async_handlers: HashMap::new(),
            handlers: HashMap::new(),
        }
    }

    pub fn async_handle(&mut self, path: Vec<u8>, handler: AsyncHttpHandler) {
        self.async_handlers.insert(path, handler);
    }
    pub fn handle(&mut self, path: Vec<u8>, handler: HttpHandler) {
        self.handlers.insert(path, handler);
    }

    pub fn run(self, addr: SocketAddr) -> Result<(), Error> {
        let mut event_driver = EventDriver::new()?;
        let route = Rc::new(self.async_handlers);
        event_driver.on_signal(Box::new(move |fd| {
            let mut handler = SignalHandler::new(fd);
            handler.register_signal_func(UnixSignal::SIGINT, Box::new(handle_shutdown));
            handler.register_signal_func(UnixSignal::SIGTERM, Box::new(handle_shutdown));
            handler.register_signal_func(UnixSignal::SIGHUP, Box::new(handle_shutdown));
            Box::new(handler)
        }));
        event_driver.on_connection(Box::new(move |fd| {
            let mut conn = HttpConnection::new(fd);
            let handlers = route.clone();
            conn.on_request(Box::new(
                move |http_ctx, request, response| -> Promise<HttpResponseWriter, String> {
                    if let Some(handler) = handlers.get(request.path()) {
                        return handler(http_ctx, request, response);
                    } else {
                        return Promise::new(move |resolve, _reject| {
                            resolve(HttpResponseWriter {
                                status_code: 404,
                                headers: HashMap::new(),
                                body: "Not Found".to_string(),
                            });
                        });
                    }
                },
            ));
            Box::new(conn)
        }));
        event_driver.listen(addr)?;
        Ok(())
    }
}

pub fn handle_shutdown(signal: UnixSignal, ctx: &mut crate::event::EventCtx) {
    match signal {
        UnixSignal::SIGINT | UnixSignal::SIGTERM | UnixSignal::SIGHUP => {
            log::info!("Received signal {:?}, shutting down.", signal);
            ctx.shutdown();
        }
        _ => {}
    }
}
