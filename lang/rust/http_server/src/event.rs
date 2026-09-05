use std::{
    cell::RefCell,
    collections::{HashMap, VecDeque},
    io::Error,
    os::fd::{AsRawFd, FromRawFd, IntoRawFd, OwnedFd},
    rc::Rc,
    sync::atomic::AtomicI32,
    time::Duration,
};

use num_enum::TryFromPrimitive;

static SIGNAL_PIPE_FD: AtomicI32 = AtomicI32::new(-1);

extern "C" fn handle_signal(signal: i32) {
    // write may modify errno, so we need to save and restore it
    let errno_ptr = unsafe { libc::__errno_location() };
    let saved_errno = unsafe { *errno_ptr };
    let byte = signal as u8;
    let pipe_fd = SIGNAL_PIPE_FD.load(std::sync::atomic::Ordering::Acquire);
    if pipe_fd != -1 {
        let _ = unsafe { libc::write(pipe_fd, &byte as *const u8 as *const libc::c_void, 1) };
    }
    unsafe {
        *errno_ptr = saved_errno;
    }
}

#[repr(i32)]
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, TryFromPrimitive)]
pub enum UnixSignal {
    SIGHUP = libc::SIGHUP,
    SIGINT = libc::SIGINT,
    SIGQUIT = libc::SIGQUIT,
    SIGILL = libc::SIGILL,
    SIGABRT = libc::SIGABRT,
    SIGFPE = libc::SIGFPE,
    SIGKILL = libc::SIGKILL,
    SIGSEGV = libc::SIGSEGV,
    SIGPIPE = libc::SIGPIPE,
    SIGALRM = libc::SIGALRM,
    SIGTERM = libc::SIGTERM,
    SIGUSR1 = libc::SIGUSR1,
    SIGUSR2 = libc::SIGUSR2,
    SIGCHLD = libc::SIGCHLD,
    SIGCONT = libc::SIGCONT,
    SIGSTOP = libc::SIGSTOP,
    SIGTSTP = libc::SIGTSTP,
    SIGTTIN = libc::SIGTTIN,
    SIGTTOU = libc::SIGTTOU,
}

bitflags::bitflags! {
    #[derive(Debug, Clone, Copy, PartialEq, Eq)]
    pub struct SignalFlags: i32 {
        const SA_NOCLDSTOP = libc::SA_NOCLDSTOP;
        const SA_NOCLDWAIT = libc::SA_NOCLDWAIT;
        const SA_NODEFER = libc::SA_NODEFER;
        const SA_ONSTACK = libc::SA_ONSTACK;
        const SA_RESETHAND = libc::SA_RESETHAND;
        const SA_RESTART = libc::SA_RESTART;
        const SA_SIGINFO = libc::SA_SIGINFO;
    }
}

pub enum SignalAction {
    Default,
    Ignore,
    Handler(extern "C" fn(libc::c_int)),
    SigAction(extern "C" fn(libc::c_int, *mut libc::siginfo_t, *mut libc::c_void)),
}
pub enum Cmd {
    Close(u64),
    AddTimer(Duration, TimerCallback),
    DisableWrite(u64),
    DisableRead(u64),
    EnableWrite(u64),
    EnableRead(u64),
    AddConnection(OwnedFd),
    Shutdown,
}

pub struct EventCtx {
    events: Interest,
    cmds: Rc<RefCell<VecDeque<Cmd>>>,
    pub handler_id: u64,
}

pub struct Sender {
    cmds: Rc<RefCell<VecDeque<Cmd>>>,
    handler_id: u64,
}

impl Sender {
    pub fn close(self) {
        self.cmds
            .borrow_mut()
            .push_back(Cmd::Close(self.handler_id));
    }

    pub fn add_timer(self, duration: Duration, callback: TimerCallback) {
        self.cmds
            .borrow_mut()
            .push_back(Cmd::AddTimer(duration, callback));
    }

    pub fn disable_write(self) {
        self.cmds
            .borrow_mut()
            .push_back(Cmd::DisableWrite(self.handler_id));
    }

    pub fn disable_read(self) {
        self.cmds
            .borrow_mut()
            .push_back(Cmd::DisableRead(self.handler_id));
    }

    pub fn enable_write(self) {
        self.cmds
            .borrow_mut()
            .push_back(Cmd::EnableWrite(self.handler_id));
    }

    pub fn enable_read(self) {
        self.cmds
            .borrow_mut()
            .push_back(Cmd::EnableRead(self.handler_id));
    }
}

impl EventCtx {
    pub fn close(&mut self) {
        self.cmds
            .borrow_mut()
            .push_back(Cmd::Close(self.handler_id));
    }

    pub fn add_timer(&mut self, duration: Duration, callback: TimerCallback) {
        self.cmds
            .borrow_mut()
            .push_back(Cmd::AddTimer(duration, callback));
    }

    pub fn disable_write(&mut self) {
        self.cmds
            .borrow_mut()
            .push_back(Cmd::DisableWrite(self.handler_id));
    }

    pub fn disable_read(&mut self) {
        self.cmds
            .borrow_mut()
            .push_back(Cmd::DisableRead(self.handler_id));
    }

    pub fn enable_write(&mut self) {
        self.cmds
            .borrow_mut()
            .push_back(Cmd::EnableWrite(self.handler_id));
    }

    pub fn enable_read(&mut self) {
        self.cmds
            .borrow_mut()
            .push_back(Cmd::EnableRead(self.handler_id));
    }

    pub fn add_connection(&mut self, fd: OwnedFd) {
        self.cmds.borrow_mut().push_back(Cmd::AddConnection(fd));
    }

    pub fn shutdown(&mut self) {
        self.cmds.borrow_mut().push_back(Cmd::Shutdown);
    }

    pub fn sender(&self) -> Sender {
        Sender {
            cmds: self.cmds.clone(),
            handler_id: self.handler_id,
        }
    }

    pub fn readable(&self) -> bool {
        self.events.contains(Interest::READABLE)
    }

    pub fn writable(&self) -> bool {
        self.events.contains(Interest::WRITABLE)
    }
}

pub trait EventHandler {
    fn fd(&self) -> &OwnedFd;
    fn on_ready(&mut self, ctx: &mut EventCtx);
    fn on_shutdown(&mut self, ctx: &mut EventCtx) {}
    fn blocks_shutdown(&self) -> bool {
        true
    }
}

pub struct SignalHandler {
    fd: OwnedFd,
    signal_funcs: HashMap<UnixSignal, Vec<SignalHandleFunc>>,
}

impl SignalHandler {
    pub fn new(fd: OwnedFd) -> Self {
        SignalHandler {
            fd,
            signal_funcs: HashMap::new(),
        }
    }

    pub fn register_signal_func(
        &mut self,
        signal: UnixSignal,
        func: SignalHandleFunc,
    ) -> Result<(), Error> {
        if let Some(funcs) = self.signal_funcs.get_mut(&signal) {
            funcs.push(Box::new(func));
        } else {
            sigaction(
                signal,
                SignalFlags::SA_RESTART,
                SignalAction::Handler(handle_signal),
            )?;
            self.signal_funcs.insert(signal, vec![Box::new(func)]);
        }
        Ok(())
    }

    pub fn handle_signal(&mut self, buf: &[u8], ctx: &mut EventCtx) {
        for &signal_byte in buf {
            if let Ok(signal) = UnixSignal::try_from(signal_byte as i32) {
                if let Some(funcs) = self.signal_funcs.get_mut(&signal) {
                    for func in funcs {
                        func(signal, ctx);
                    }
                } else {
                    log::warn!("No handler registered for signal {:?}", signal);
                }
            } else {
                log::error!("Unknown signal received: {}", signal_byte);
            }
        }
    }
}

impl EventHandler for SignalHandler {
    fn fd(&self) -> &OwnedFd {
        &self.fd
    }

    fn on_ready(&mut self, ctx: &mut EventCtx) {
        let mut buf = [0u8; 64];
        loop {
            match read(&self.fd, &mut buf) {
                Ok(0) => {
                    log::error!("Signal pipe closed");
                    ctx.close();
                    return;
                }
                Ok(n) => {
                    self.handle_signal(&buf[..n], ctx);
                }
                Err(e) if e.raw_os_error() == Some(libc::EAGAIN) => {
                    return;
                }
                Err(e) if e.raw_os_error() == Some(libc::EINTR) => {
                    log::info!("Signal pipe read interrupted by signal, retrying");
                    continue;
                }
                Err(e) => {
                    log::error!("Error reading from signal pipe: {}", e);
                    ctx.close();
                    return;
                }
            }
        }
    }

    fn blocks_shutdown(&self) -> bool {
        false
    }
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
                    ctx.add_connection(conn);
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

    fn on_shutdown(&mut self, ctx: &mut EventCtx) {
        ctx.close();
    }
}

pub enum ConnectionState {
    ReadingRequest,
    WritingResponse,
    Idel,
}

pub struct Connection {
    state: ConnectionState,
    fd: OwnedFd,
}

impl Connection {
    pub fn new(fd: OwnedFd) -> Self {
        Connection {
            state: ConnectionState::Idel,
            fd,
        }
    }
}

impl EventHandler for Connection {
    fn fd(&self) -> &OwnedFd {
        &self.fd
    }

    fn on_ready(&mut self, ctx: &mut EventCtx) {
        self.state = ConnectionState::ReadingRequest;
        let mut buf = [0u8; 1024];
        loop {
            match read(&self.fd, &mut buf) {
                Ok(n) => {
                    if n == 0 {
                        ctx.close();
                        return;
                    }
                    log::info!("Read {} bytes: {:?}", n, String::from_utf8_lossy(&buf[..n]));
                }
                Err(e) => match e.raw_os_error() {
                    Some(libc::EAGAIN) => {
                        return;
                    }
                    Some(libc::ECONNRESET) => {
                        ctx.close();
                        return;
                    }
                    Some(libc::EOF) => {
                        ctx.close();
                        return;
                    }
                    _ => {
                        ctx.close();
                        return;
                    }
                },
            }
        }
    }

    fn on_shutdown(&mut self, ctx: &mut EventCtx) {
        match self.state {
            ConnectionState::ReadingRequest => {
                return;
            }
            ConnectionState::WritingResponse => {
                ctx.disable_read();
                return;
            }
            ConnectionState::Idel => {
                ctx.close();
                return;
            }
        }
    }
}

type ListenerFunc = Box<dyn Fn(OwnedFd) -> Box<dyn EventHandler>>;
type ConnectionFunc = Box<dyn Fn(OwnedFd) -> Box<dyn EventHandler>>;
type SignalFunc = Box<dyn Fn(OwnedFd) -> Box<dyn EventHandler>>;
type SignalHandleFunc = Box<dyn Fn(UnixSignal, &mut EventCtx)>;

struct HandlerEntry {
    handler: Box<dyn EventHandler>,
    interest: Interest,
}

pub struct EventDriver {
    shutting_down: bool,
    epoll_fd: OwnedFd,
    handlers: HashMap<u64, HandlerEntry>,
    listener_func: Option<ListenerFunc>,
    connection_func: Option<ConnectionFunc>,
    event_size: Option<usize>,
    cmds: Rc<RefCell<VecDeque<Cmd>>>,
    event_seq: u64,
    signal_func: Option<SignalFunc>,
}

const DEFAULT_EPOLL_SIZE: usize = 1024;

impl EventDriver {
    pub fn new() -> Result<Self, Error> {
        let epoll_fd = epoll_create()?;
        Ok(EventDriver {
            shutting_down: false,
            epoll_fd,
            handlers: HashMap::new(),
            event_size: None,
            cmds: Rc::new(RefCell::new(VecDeque::new())),
            event_seq: 0,
            listener_func: None,
            connection_func: None,
            signal_func: None,
        })
    }

    pub fn on_listener(&mut self, func: ListenerFunc) {
        self.listener_func = Some(Box::new(func));
    }

    pub fn on_connection(&mut self, func: ConnectionFunc) {
        self.connection_func = Some(Box::new(func));
    }

    pub fn on_signal(&mut self, func: SignalFunc) {
        self.signal_func = Some(Box::new(func));
    }

    pub fn get_handler_id(&mut self) -> u64 {
        self.event_seq += 1 % 0xFFFFFFFFFFFFFFFF;
        self.event_seq
    }

    pub fn init_signal_pipe(&mut self) -> Result<(), Error> {
        let (read_fd, write_fd) = pipe2(PipeFlags::O_NONBLOCK | PipeFlags::O_CLOEXEC)?;
        // register write fd to SIGNAL_PIPE_FD, so that the signal handler can write to it
        SIGNAL_PIPE_FD.store(write_fd.into_raw_fd(), std::sync::atomic::Ordering::Release);
        let handler_id = self.get_handler_id();
        // ignore SIGPIPE, so that the process won't be killed when writing to a closed socket
        sigaction(
            UnixSignal::SIGPIPE,
            SignalFlags::empty(),
            SignalAction::Ignore,
        )?;
        epoll_ctl(
            &self.epoll_fd,
            EpollOP::Add {
                event: Interest::READABLE | Interest::EDGE_TRIGGERED,
                user_data: handler_id,
            },
            &read_fd,
        )?;
        if let Some(func) = &self.signal_func {
            self.handlers.insert(
                handler_id,
                HandlerEntry {
                    handler: func(read_fd),
                    interest: Interest::READABLE | Interest::EDGE_TRIGGERED,
                },
            );
        }
        Ok(())
    }

    pub fn listen(&mut self, addr: std::net::SocketAddr) -> Result<(), Error> {
        let socket = tcp_socket()?;
        setsockopt(&socket, libc::SOL_SOCKET, libc::SO_REUSEADDR, true)?;
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
        self.handlers.insert(
            handler_id,
            HandlerEntry {
                handler: listener_func(socket),
                interest: Interest::READABLE | Interest::EDGE_TRIGGERED,
            },
        );
        self.init_signal_pipe()?;
        let mut events = if let Some(size) = self.event_size {
            vec![EpollEvent::zeroed(); size]
        } else {
            vec![EpollEvent::zeroed(); DEFAULT_EPOLL_SIZE]
        };
        loop {
            if  self.shutting_down {
                if self.handlers.is_empty() || self.handlers.values().all(|entry| !entry.handler.blocks_shutdown()) {
                    return Ok(());
                }
            }
            match epoll_wait(&self.epoll_fd, &mut events, -1) {
                Ok(n) => {
                    if n == 0 {
                        continue;
                    }
                    self.handle_events(&mut events[..n])?;
                }
                Err(e) if e.raw_os_error() == Some(libc::EINTR) => {
                    log::info!("epoll_wait interrupted by signal, retrying");
                    continue;
                }
                Err(e) => {
                    return Err(e);
                }
            }
        }
    }

    pub fn handle_events(&mut self, events: &mut [EpollEvent]) -> Result<(), Error> {
        for event in events {
            self.handle_event(event)?;
        }
        Ok(())
    }
    pub fn handle_event(&mut self, event: &EpollEvent) -> Result<(), Error> {
        let handler_id = event.user_data();
        let entry = self.handlers.get_mut(&handler_id);
        let ctx = &mut EventCtx {
            events: event.events(),
            cmds: self.cmds.clone(),
            handler_id,
        };
        match entry {
            Some(entry) => {
                entry.handler.on_ready(ctx);
            }
            None => {
                log::error!("Handler not found for handler_id: {}", handler_id);
            }
        }
        self.handle_cmds()
    }

    fn update_interest<F>(&mut self, handler_id: u64, update: F) -> Result<(), Error>
    where
        F: FnOnce(&mut Interest),
    {
        let Some(entry) = self.handlers.get_mut(&handler_id) else {
            return Ok(());
        };

        let previous = entry.interest;
        update(&mut entry.interest);

        if entry.interest == previous {
            return Ok(());
        }

        if let Err(error) = epoll_ctl(
            &self.epoll_fd,
            EpollOP::Mod {
                event: entry.interest,
                user_data: handler_id,
            },
            entry.handler.fd(),
        ) {
            entry.interest = previous;
            return Err(error);
        }

        Ok(())
    }

    pub fn handle_cmds(&mut self) -> Result<(), Error> {
        loop {
            let cmd = {
                let mut cmds = self.cmds.borrow_mut();
                cmds.pop_front()
            };
            let Some(cmd) = cmd else {
                break;
            };
            match cmd {
                Cmd::Close(handler_id) => {
                    if let Some(entry) = self.handlers.remove(&handler_id) {
                        if let Err(e) = epoll_ctl(&self.epoll_fd, EpollOP::Del, entry.handler.fd())
                        {
                            log::error!("Failed to remove handler from epoll: {}", e);
                        }
                    }
                    log::info!("remove handler: {}", handler_id);
                }
                Cmd::AddConnection(fd) => {
                    let handler_id = self.get_handler_id();
                    epoll_ctl(
                        &self.epoll_fd,
                        EpollOP::Add {
                            event: Interest::READABLE | Interest::EDGE_TRIGGERED,
                            user_data: handler_id,
                        },
                        &fd,
                    )?;
                    let connection_func = match &self.connection_func {
                        Some(func) => func.as_ref(),
                        None => {
                            &|fd: OwnedFd| Box::new(Connection::new(fd)) as Box<dyn EventHandler>
                        }
                    };
                    self.handlers.insert(
                        handler_id,
                        HandlerEntry {
                            handler: connection_func(fd),
                            interest: Interest::READABLE | Interest::EDGE_TRIGGERED,
                        },
                    );
                }
                Cmd::Shutdown => {
                    self.shutting_down = true;
                    for (handler_id, entry) in self.handlers.iter_mut() {
                        entry.handler.on_shutdown(&mut EventCtx {
                            events: Interest::READABLE
                                | Interest::WRITABLE
                                | Interest::EDGE_TRIGGERED,
                            cmds: self.cmds.clone(),
                            handler_id: *handler_id,
                        });
                    }
                }
                Cmd::DisableWrite(id) => {
                    self.update_interest(id, |interest| {
                        interest.remove(Interest::WRITABLE);
                    })?;
                }
                Cmd::DisableRead(id) => {
                    self.update_interest(id, |interest| {
                        interest.remove(Interest::READABLE);
                    })?;
                }
                Cmd::AddTimer(duration, callback) => {
                    let timerfd = create_timerfd()?;
                    set_timer_timeout(&timerfd, get_absolute_time_ms(duration))?;
                    let handler_id = self.get_handler_id();
                    epoll_ctl(
                        &self.epoll_fd,
                        EpollOP::Add {
                            event: Interest::READABLE | Interest::EDGE_TRIGGERED,
                            user_data: handler_id,
                        },
                        &timerfd,
                    )?;
                    self.handlers.insert(
                        handler_id,
                        HandlerEntry {
                            handler: Box::new(TimeHandler::new(timerfd, callback)),
                            interest: Interest::READABLE | Interest::EDGE_TRIGGERED,
                        },
                    );
                }
                Cmd::EnableWrite(handler_id) => {
                    self.update_interest(handler_id, |interest| {
                        interest.insert(Interest::WRITABLE);
                    })?;
                }
                Cmd::EnableRead(handler_id) => {
                    self.update_interest(handler_id, |interest| {
                        interest.insert(Interest::READABLE);
                    })?;
                }
            }
        }
        Ok(())
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

pub fn setsockopt(socket: &OwnedFd, level: i32, optname: i32, enable: bool) -> Result<(), Error> {
    let optval: i32 = if enable { 1 } else { 0 };
    let ret = unsafe {
        libc::setsockopt(
            socket.as_raw_fd(),
            level,
            optname,
            &optval as *const i32 as *const libc::c_void,
            std::mem::size_of::<i32>() as libc::socklen_t,
        )
    };
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

pub fn sigaction(
    signal: UnixSignal,
    flags: SignalFlags,
    handler: SignalAction,
) -> Result<(), Error> {
    let mut sig_action: libc::sigaction = unsafe { std::mem::zeroed() };
    let (f_ptr, sig_flag) = match handler {
        SignalAction::Default => (libc::SIG_DFL, 0),
        SignalAction::Ignore => (libc::SIG_IGN, 0),
        SignalAction::Handler(h) => (h as usize, libc::SA_SIGINFO),
        SignalAction::SigAction(h) => (h as usize, libc::SA_SIGINFO),
    };
    sig_action.sa_sigaction = f_ptr;
    sig_action.sa_flags = flags.bits() | sig_flag;
    unsafe { libc::sigemptyset(&mut sig_action.sa_mask) };
    let ret = unsafe { libc::sigaction(signal as i32, &sig_action, std::ptr::null_mut()) };
    if ret != 0 {
        return Err(std::io::Error::last_os_error());
    }
    Ok(())
}

bitflags::bitflags! {
    #[derive(Debug, Clone, Copy, PartialEq, Eq)]
    pub struct PipeFlags: i32 {
        const O_NONBLOCK = libc::O_NONBLOCK;
        const O_CLOEXEC = libc::O_CLOEXEC;
    }
}

pub fn pipe2(flags: PipeFlags) -> Result<(OwnedFd, OwnedFd), Error> {
    let fds = &mut [0; 2];
    let ret = unsafe { libc::pipe2(fds.as_ptr() as *mut i32, flags.bits()) };
    if ret != 0 {
        return Err(std::io::Error::last_os_error());
    }
    Ok((unsafe { OwnedFd::from_raw_fd(fds[0]) }, unsafe {
        OwnedFd::from_raw_fd(fds[1])
    }))
}

pub fn create_timerfd() -> Result<OwnedFd, Error> {
    let fd = unsafe {
        libc::timerfd_create(
            libc::CLOCK_MONOTONIC,
            libc::TFD_NONBLOCK | libc::TFD_CLOEXEC,
        )
    };
    if fd < 0 {
        return Err(std::io::Error::last_os_error());
    }
    Ok(unsafe { OwnedFd::from_raw_fd(fd) })
}

pub fn set_timer_timeout(fd: &OwnedFd, timeout_ms: u64) -> Result<(), Error> {
    let new_value = libc::itimerspec {
        it_interval: libc::timespec {
            tv_sec: 0,
            tv_nsec: 0,
        },
        it_value: libc::timespec {
            tv_sec: (timeout_ms / 1000) as libc::time_t,
            tv_nsec: ((timeout_ms % 1000) * 1_000_000) as libc::c_long,
        },
    };
    let ret = unsafe { libc::timerfd_settime(fd.as_raw_fd(), libc::TFD_TIMER_ABSTIME, &new_value, std::ptr::null_mut()) };
    if ret != 0 {
        return Err(std::io::Error::last_os_error());
    }
    Ok(())
}

pub fn monotonic_now_ns() -> u64 {
    let mut ts: libc::timespec = unsafe { std::mem::zeroed() };
    let ret = unsafe { libc::clock_gettime(libc::CLOCK_MONOTONIC, &mut ts) };
    if ret != 0 {
        panic!("clock_gettime failed: {}", std::io::Error::last_os_error());
    }
    (ts.tv_sec as u64) * 1_000_000_000 + (ts.tv_nsec as u64)
}

pub fn get_absolute_time_ms(duration: Duration) -> u64 {
    monotonic_now_ns() / 1_000_000 + duration.as_millis() as u64
}

pub type TimerCallback = Box<dyn FnOnce() + 'static>;
pub struct TimeHandler {
    fd: OwnedFd,
    callback: Option<TimerCallback>,
}

impl TimeHandler {
    pub fn new(fd: OwnedFd, callback: TimerCallback) -> Self {
        Self {
            fd,
            callback: Some(callback),
        }
    }
}

impl EventHandler for TimeHandler {
    fn fd(&self) -> &std::os::unix::prelude::OwnedFd {
        &self.fd
    }

    fn on_ready(&mut self, ctx: &mut crate::event::EventCtx) {
        // todo: implement the timer logic here
        if let Some(callback) = self.callback.take() {
            callback();
        }
        ctx.close();
    }
}
