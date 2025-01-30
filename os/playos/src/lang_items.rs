use core::panic::PanicInfo;
use crate::sbi::shutdown;

#[panic_handler]
fn panic(info: &PanicInfo) -> !{
    if let Some(location) = info.location() {
        println!(
            "paniced at {}:{} {}",
            location.file(),
            location.line(),
            info.message().unwrap(),
                );
    }else{
        println!("paniced {}",info.message().unwrap());
    }
    shutdown();
}

