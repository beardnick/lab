#![no_std]
#![no_main]
#![feature(panic_info_message)]

#[macro_use]
mod console;
mod lang_items;
mod sbi;

use core::arch::global_asm;

global_asm!(include_str!("entry.asm"));

// no_mangle avoid confusing rust_main name
// rust_main name will keep in asm
#[no_mangle]
pub fn rust_main() -> !{
    clear_bss();
    println!("hello world");
    panic!("shutdown");
}

//#[no_mangle]
//pub extern "C" fn _start() -> ! {
//    clear_bss();
//    println!("hello world");
//    panic!("shutdown");
//}


pub fn clear_bss(){
    extern "C" {
         // find sbss ebss symbol from extern program,  provided by linker.ld
        fn sbss();
        fn ebss();
    }
    // rust range expression
    // write 0 to range [sbss , ebss)
    (sbss as usize .. ebss as usize).for_each(|a|{
        unsafe {(a as *mut u8).write_volatile(0)}
    })
}

