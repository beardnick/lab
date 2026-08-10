use crate::{
    event::{Cmd, EventDriver, EventHandler, read, write},
    http_parser::{ParseStatus, Parser, Request, Response},
};
use std::{collections::HashMap, io::Error, net::SocketAddr, os::fd::OwnedFd, rc::Rc};

type OnRequestCallback = Box<dyn Fn(&mut Request, &mut HttpResponseWriter)>;
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
    fd: OwnedFd,
    parser: Parser,
    // the connection owns the byte stream; the parser only keeps positions into it
    read_buf: Vec<u8>,
    on_request: Option<OnRequestCallback>,
    on_log: Option<OnLogCallback>,
}

impl HttpConnection {
    pub fn new(fd: OwnedFd) -> Self {
        Self {
            fd,
            parser: Parser::default(),
            read_buf: Vec::new(),
            on_request: None,
            on_log: Some(Box::new(|request, response| {
                log::debug!("Request: {:?}, Response: {:?}", request, response);
            })),
        }
    }
    pub fn on_request(&mut self, callback: OnRequestCallback) {
        self.on_request = Some(callback);
    }
    pub fn on_log(&mut self, callback: OnLogCallback) {
        self.on_log = Some(callback);
    }
}

impl EventHandler for HttpConnection {
    fn fd(&self) -> &std::os::unix::prelude::OwnedFd {
        &self.fd
    }

    fn on_ready(&mut self, ctx: &mut crate::event::EventCtx) {
        // 1. drain the socket into read_buf (edge-triggered: must read to EAGAIN)
        let mut buf = [0u8; 1024];
        loop {
            match read(&self.fd, &mut buf) {
                Ok(0) => {
                    // peer closed
                    ctx.cmds.push_back(Cmd::Close(ctx.handler_id));
                    return;
                }
                Ok(n) => self.read_buf.extend_from_slice(&buf[..n]),
                Err(e) if e.raw_os_error() == Some(libc::EAGAIN) => break,
                Err(e) => {
                    log::debug!("read error, closing: {e}");
                    ctx.cmds.push_back(Cmd::Close(ctx.handler_id));
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
                        callback(&mut request, &mut response);
                        if let Err(e) = write(&self.fd, &response.endcode()) {
                            log::warn!("write error, closing: {e}");
                            ctx.cmds.push_back(Cmd::Close(ctx.handler_id));
                            return;
                        }
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
                    ctx.cmds.push_back(Cmd::Close(ctx.handler_id));
                    return;
                }
            }
        }
    }
}

pub fn new_http_connection(fd: OwnedFd) -> Box<dyn EventHandler> {
    Box::new(HttpConnection::new(fd))
}

pub type HttpHandler = Box<dyn Fn(&mut Request, &mut HttpResponseWriter)>;

pub struct HttpServer {
    handlers: HashMap<Vec<u8>, HttpHandler>,
}

impl HttpServer {
    pub fn new() -> Self {
        Self {
            handlers: HashMap::new(),
        }
    }

    pub fn handle(&mut self, path: Vec<u8>, handler: HttpHandler) {
        self.handlers.insert(path, handler);
    }

    pub fn run(self, addr: SocketAddr) -> Result<(), Error> {
        let mut event_driver = EventDriver::new()?;
        let route = Rc::new(self.handlers);
        event_driver.on_connection(Box::new(move |fd| {
            let mut conn = HttpConnection::new(fd);
            let handlers = route.clone();
            conn.on_request(Box::new(move |request, response| {
                if let Some(handler) = handlers.get(request.path()) {
                    handler(request, response);
                } else {
                    response.status_code = 404;
                    response.body = "Not Found".to_string();
                }
            }));
            Box::new(conn)
        }));
        event_driver.listen(addr)?;
        Ok(())
    }
}
