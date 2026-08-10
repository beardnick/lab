use std::{
    collections::{HashMap, VecDeque},
    io::Error,
    os::fd::{AsRawFd, FromRawFd, OwnedFd},
};

pub enum Cmd {
    Close(u64),
    AddConnection(OwnedFd),
}
pub struct EventCtx<'a> {
    pub cmds: &'a mut VecDeque<Cmd>,
    pub handler_id: u64,
}

pub trait EventHandler {
    fn fd(&self) -> &OwnedFd;
    fn on_ready(&mut self, ctx: &mut EventCtx);
}

pub struct Listener {
    fd: OwnedFd,
}

impl Listener {
    pub fn new(fd: OwnedFd) -> Self {
        Listener { fd }
    }
}

impl EventHandler for Listener {
    fn fd(&self) -> &OwnedFd {
        &self.fd
    }

    fn on_ready(&mut self, ctx: &mut EventCtx) {
        loop {
            match accept_connection_non_block(&self.fd) {
                Ok((conn, addr)) => {
                    ctx.cmds.push_back(Cmd::AddConnection(conn));
                    log::info!("Accepted connection from {}", addr);
                }
                Err(e) => match e.raw_os_error() {
                    Some(libc::EAGAIN) => {
                        return;
                    }
                    _ => {
                        log::error!("Error accepting connection: {}", e);
                        return;
                    }
                },
            }
        }
    }
}

pub struct Connection {
    fd: OwnedFd,
}

impl Connection {
    pub fn new(fd: OwnedFd) -> Self {
        Connection { fd }
    }
}

impl EventHandler for Connection {
    fn fd(&self) -> &OwnedFd {
        &self.fd
    }

    fn on_ready(&mut self, ctx: &mut EventCtx) {
        let mut buf = [0u8; 1024];
        loop {
            match read(&self.fd, &mut buf) {
                Ok(n) => {
                    if n == 0 {
                        ctx.cmds.push_back(Cmd::Close(ctx.handler_id));
                        return;
                    }
                    log::info!("Read {} bytes: {:?}", n, String::from_utf8_lossy(&buf[..n]));
                }
                Err(e) => match e.raw_os_error() {
                    Some(libc::EAGAIN) => {
                        return;
                    }
                    Some(libc::ECONNRESET) => {
                        ctx.cmds.push_back(Cmd::Close(ctx.handler_id));
                        return;
                    }
                    Some(libc::EOF) => {
                        ctx.cmds.push_back(Cmd::Close(ctx.handler_id));
                        return;
                    }
                    _ => {
                        ctx.cmds.push_back(Cmd::Close(ctx.handler_id));
                        return;
                    }
                },
            }
        }
    }
}

type ListenerFunc = Box<dyn Fn(OwnedFd) -> Box<dyn EventHandler>>;
type ConnectionFunc = Box<dyn Fn(OwnedFd) -> Box<dyn EventHandler>>;

pub struct EventDriver {
    epoll_fd: OwnedFd,
    handlers: HashMap<u64, Box<dyn EventHandler>>,
    listener_func: Option<ListenerFunc>,
    connection_func: Option<ConnectionFunc>,
    event_size: Option<usize>,
    cmds: VecDeque<Cmd>,
    event_seq: u64,
}

const DEFAULT_EPOLL_SIZE: usize = 1024;

impl EventDriver {
    pub fn new() -> Result<Self, Error> {
        let epoll_fd = epoll_create()?;
        Ok(EventDriver {
            epoll_fd,
            handlers: HashMap::new(),
            event_size: None,
            cmds: VecDeque::new(),
            event_seq: 0,
            listener_func: None,
            connection_func: None,
        })
    }

    pub fn on_listener(&mut self, func: ListenerFunc) {
        self.listener_func = Some(Box::new(func));
    }

    pub fn on_connection(&mut self, func: ConnectionFunc) {
        self.connection_func = Some(Box::new(func));
    }

    pub fn get_handler_id(&mut self) -> u64 {
        self.event_seq += 1 % 0xFFFFFFFFFFFFFFFF;
        self.event_seq
    }

    pub fn listen(&mut self, addr: std::net::SocketAddr) -> Result<(), Error> {
        let socket = tcp_socket()?;
        bind_socket(&socket, addr)?;
        listen(&socket)?;
        log::info!("Listening on {}", addr);
        let handler_id = self.get_handler_id();
        epoll_ctl(
            &self.epoll_fd,
            EpollOP::Add {
                event: Interest::READABLE | Interest::EDGE_TRIGGERED,
                user_data: handler_id,
            },
            &socket,
        )?;
        let listener_func = match &self.listener_func {
            Some(func) => func.as_ref(),
            None => &|fd: OwnedFd| Box::new(Listener::new(fd)) as Box<dyn EventHandler>,
        };
        self.handlers.insert(handler_id, listener_func(socket));
        let mut events = if let Some(size) = self.event_size {
            vec![EpollEvent::zeroed(); size]
        } else {
            vec![EpollEvent::zeroed(); DEFAULT_EPOLL_SIZE]
        };
        loop {
            let n = epoll_wait(&self.epoll_fd, &mut events, -1)?;
            if n == 0 {
                continue;
            }
            self.handle_events(&mut events[..n]);
        }
        Ok(())
    }

    pub fn handle_events(&mut self, events: &mut [EpollEvent]) {
        for event in events {
            self.handle_event(event);
        }
    }
    pub fn handle_event(&mut self, event: &EpollEvent) {
        let handler_id = event.user_data();
        let handler = self.handlers.get_mut(&handler_id);
        let ctx = &mut EventCtx {
            cmds: &mut self.cmds,
            handler_id,
        };
        match handler {
            Some(handler) => {
                handler.on_ready(ctx);
            }
            None => {
                log::error!("Handler not found for handler_id: {}", handler_id);
            }
        }

        while let Some(cmd) = self.cmds.pop_front() {
            match cmd {
                Cmd::Close(handler_id) => {
                    if let Some(handler) = self.handlers.remove(&handler_id) {
                        if let Err(e) = epoll_ctl(&self.epoll_fd, EpollOP::Del, handler.fd()) {
                            log::error!("Failed to remove handler from epoll: {}", e);
                        }
                    }
                    log::info!("Closed connection with handler_id: {}", handler_id);
                }
                Cmd::AddConnection(fd) => {
                    let handler_id = self.get_handler_id();
                    epoll_ctl(
                        &self.epoll_fd,
                        EpollOP::Add {
                            event: Interest::READABLE
                                | Interest::WRITABLE
                                | Interest::EDGE_TRIGGERED,
                            user_data: handler_id,
                        },
                        &fd,
                    )
                    .unwrap();
                    let connection_func = match &self.connection_func {
                        Some(func) => func.as_ref(),
                        None => {
                            &|fd: OwnedFd| Box::new(Connection::new(fd)) as Box<dyn EventHandler>
                        }
                    };
                    self.handlers.insert(handler_id, connection_func(fd));
                }
            }
        }
    }
}

pub fn accept_connection_non_block(
    socket: &OwnedFd,
) -> Result<(OwnedFd, std::net::SocketAddrV4), Error> {
    let mut addr: libc::sockaddr_in = unsafe { std::mem::zeroed() };
    let mut addr_len = std::mem::size_of::<libc::sockaddr_in>() as libc::socklen_t;
    let conn_fd = unsafe {
        libc::accept4(
            socket.as_raw_fd(),
            &mut addr as *mut libc::sockaddr_in as *mut libc::sockaddr,
            &mut addr_len,
            libc::SOCK_NONBLOCK,
        )
    };
    if conn_fd < 0 {
        return Err(std::io::Error::last_os_error());
    }
    let addr_v4 = std::net::SocketAddrV4::new(
        std::net::Ipv4Addr::from(u32::from_be(addr.sin_addr.s_addr)),
        u16::from_be(addr.sin_port),
    );
    Ok((unsafe { OwnedFd::from_raw_fd(conn_fd) }, addr_v4))
}

pub fn tcp_socket() -> Result<OwnedFd, Error> {
    let socket_fd = unsafe {
        libc::socket(
            libc::AF_INET,
            libc::SOCK_STREAM | libc::SOCK_NONBLOCK | libc::SOCK_CLOEXEC,
            0,
        )
    };
    if socket_fd < 0 {
        return Err(std::io::Error::last_os_error());
    }
    Ok(unsafe { OwnedFd::from_raw_fd(socket_fd) })
}

pub fn listen(socket: &OwnedFd) -> Result<(), Error> {
    let ret = unsafe { libc::listen(socket.as_raw_fd(), 128) };
    if ret < 0 {
        return Err(std::io::Error::last_os_error());
    }
    Ok(())
}

pub fn bind_socket(socket: &OwnedFd, addr: std::net::SocketAddr) -> Result<(), Error> {
    let v4addr = match addr.ip() {
        std::net::IpAddr::V4(ipv4_addr) => ipv4_addr,
        std::net::IpAddr::V6(ipv6_addr) => {
            return Err(std::io::Error::new(
                std::io::ErrorKind::InvalidInput,
                "IPv6 is not supported",
            ));
        }
    };

    let sockaddr_in = libc::sockaddr_in {
        sin_family: libc::AF_INET as u16,
        sin_port: addr.port().to_be(),
        sin_addr: libc::in_addr {
            s_addr: libc::htonl(v4addr.into()),
        },
        sin_zero: [0; 8],
    };
    let ret = unsafe {
        libc::bind(
            socket.as_raw_fd(),
            &sockaddr_in as *const libc::sockaddr_in as *const libc::sockaddr,
            std::mem::size_of::<libc::sockaddr_in>() as u32,
        )
    };
    if ret < 0 {
        return Err(std::io::Error::last_os_error());
    }
    Ok(())
}

pub fn epoll_wait(epfd: &OwnedFd, events: &mut [EpollEvent], timeout: i32) -> Result<usize, Error> {
    let ret = unsafe {
        libc::epoll_wait(
            epfd.as_raw_fd(),
            events.as_mut_ptr() as *mut libc::epoll_event,
            events.len() as i32,
            timeout,
        )
    };
    if ret < 0 {
        return Err(std::io::Error::last_os_error());
    }
    Ok(ret as usize)
}

pub fn epoll_create() -> Result<OwnedFd, Error> {
    let epoll_fd = unsafe { libc::epoll_create1(libc::EPOLL_CLOEXEC) };
    if epoll_fd < 0 {
        return Err(std::io::Error::last_os_error());
    }
    Ok(unsafe { OwnedFd::from_raw_fd(epoll_fd) })
}

bitflags::bitflags! {
    #[derive(Debug, Clone, Copy, PartialEq, Eq)]
    pub struct Interest: u32 {
        const READABLE       = libc::EPOLLIN as u32;
        const WRITABLE       = libc::EPOLLOUT as u32;
        const EDGE_TRIGGERED = libc::EPOLLET as u32;
        const ONESHOT        = libc::EPOLLONESHOT as u32;
        const PEER_CLOSED    = libc::EPOLLRDHUP as u32;
        const ERROR          = libc::EPOLLERR as u32;
        const HANGUP         = libc::EPOLLHUP as u32;
    }
}

#[repr(transparent)]
#[derive(Debug, Clone, Copy)]
pub struct EpollEvent(libc::epoll_event);

impl EpollEvent {
    pub fn zeroed() -> Self {
        EpollEvent(libc::epoll_event { events: 0, u64: 0 })
    }
    pub fn events(&self) -> Interest {
        Interest::from_bits_truncate(self.0.events)
    }

    pub fn user_data(&self) -> u64 {
        self.0.u64
    }

    pub fn new(events: Interest, user_data: u64) -> Self {
        EpollEvent(libc::epoll_event {
            events: events.bits(),
            u64: user_data,
        })
    }
}

enum EpollOP {
    Add { event: Interest, user_data: u64 },
    Mod { event: Interest, user_data: u64 },
    Del,
}

pub fn epoll_ctl(epfd: &OwnedFd, op: EpollOP, target: &OwnedFd) -> Result<(), Error> {
    let (raw_op, mut event) = match op {
        EpollOP::Add { event, user_data } => (
            libc::EPOLL_CTL_ADD,
            Some(libc::epoll_event {
                events: event.bits(),
                u64: user_data,
            }),
        ),
        EpollOP::Mod { event, user_data } => (
            libc::EPOLL_CTL_MOD,
            Some(libc::epoll_event {
                events: event.bits(),
                u64: user_data,
            }),
        ),

        EpollOP::Del => (libc::EPOLL_CTL_DEL, None),
    };

    let event_ptr = match event.as_mut() {
        Some(e) => e as *mut libc::epoll_event,
        None => std::ptr::null_mut(),
    };
    let ret = unsafe { libc::epoll_ctl(epfd.as_raw_fd(), raw_op, target.as_raw_fd(), event_ptr) };
    if ret != 0 {
        return Err(std::io::Error::last_os_error());
    }
    Ok(())
}

pub fn read(fd: &OwnedFd, buf: &mut [u8]) -> Result<usize, Error> {
    let ret = unsafe {
        libc::read(
            fd.as_raw_fd(),
            buf.as_mut_ptr() as *mut libc::c_void,
            buf.len(),
        )
    };
    if ret < 0 {
        return Err(std::io::Error::last_os_error());
    }
    Ok(ret as usize)
}

pub fn write(fd: &OwnedFd, buf: &[u8]) -> Result<usize, Error> {
    let ret = unsafe {
        libc::write(
            fd.as_raw_fd(),
            buf.as_ptr() as *const libc::c_void,
            buf.len(),
        )
    };
    if ret < 0 {
        return Err(std::io::Error::last_os_error());
    }
    Ok(ret as usize)
}
