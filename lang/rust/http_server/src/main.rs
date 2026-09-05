mod event;
mod http_parser;
mod http_server;
mod promise;
mod signal;

use core::time;
use http_server::{HttpResponseWriter, HttpServer};
use promise::Promise;
use std::net::SocketAddr;

fn main() {
    env_logger::Builder::from_env(env_logger::Env::default().default_filter_or("info")).init();
    let mut server = HttpServer::new();
    server.handle(
        String::from("/hello").into_bytes(),
        Box::new(|ctx, req, res| {
            res.status_code = 200;
            res.body = String::from("Hello, world!");
        }),
    );
    server.async_handle(
        String::from("/slow").into_bytes(),
        Box::new(|ctx, req, res| -> Promise<HttpResponseWriter, String> {
            Promise::<HttpResponseWriter, String>::new(move |resolve, reject| {
                ctx.set_timeout(
                    time::Duration::from_secs(10),
                    Box::new(move || {
                        let resp = HttpResponseWriter {
                            status_code: 200,
                            headers: Default::default(),
                            body: String::from("slow hello"),
                        };
                        resolve(resp);
                    }),
                );
            })
        }),
    );
    server
        .run(SocketAddr::from(([127, 0, 0, 1], 18080)))
        .unwrap();
}
