use std::net::SocketAddr;

use http_server::{HttpResponseWriter, HttpServer};

mod event;
mod http_parser;
mod http_server;

fn main() {
    env_logger::Builder::from_env(env_logger::Env::default().default_filter_or("info")).init();
    let mut server = HttpServer::new();
    server.handle(
        String::from("/hello").into_bytes(),
        Box::new(|req, res| {
            res.status_code = 200;
            res.body = String::from("Hello, world!");
        }),
    );
    server
        .run(SocketAddr::from(([127, 0, 0, 1], 18080)))
        .unwrap();
}
