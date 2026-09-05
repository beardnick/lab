use std::{cell::RefCell, rc::Rc};

pub type Reject<E> = Box<dyn FnOnce(E)>;

pub type Resolve<T> = Box<dyn FnOnce(T)>;

//pub type PromiseCallback<T, E> = FnOnce(Resolve<T>, Reject<E>);

pub type Then<V, T> = Box<dyn FnOnce(V) -> T>;

pub type Continuation<T, E> = Box<dyn FnOnce(Result<T, E>)>;

struct PromiseInner<T, E> {
    result: Option<Result<T, E>>,
    resolved: bool,
    next: Option<Continuation<T, E>>,
}

pub struct Promise<T, E> {
    inner: Option<Rc<RefCell<PromiseInner<T, E>>>>,
}

impl<T, E> Promise<T, E>
where
    T: 'static,
    E: 'static,
{
    pub fn new<F>(callback: F) -> Self
    where
        F: FnOnce(Resolve<T>, Reject<E>),
    {
        let (resolve, reject, p) = Self::pending();
        callback(resolve, reject);
        p
    }

    pub fn pending() -> (Resolve<T>, Reject<E>, Self) {
        let inner = Rc::new(RefCell::new(PromiseInner {
            result: None,
            resolved: false,
            next: None,
        }));
        let inner_for_resolve = inner.clone();
        let resolve = Box::new(move |value: T| {
            inner_for_resolve.borrow_mut().resolved = true;
            let next = {
                let mut inner = inner_for_resolve.borrow_mut();
                if let Some(next) = inner.next.take() {
                    next
                } else {
                    inner.result = Some(Ok(value));
                    return;
                }
            };
            next(Ok(value));
        });

        let inner_for_reject = inner.clone();

        let reject = Box::new(move |error: E| {
            inner_for_reject.borrow_mut().resolved = true;
            let next = {
                let mut inner = inner_for_reject.borrow_mut();
                if let Some(next) = inner.next.take() {
                    next
                } else {
                    inner.result = Some(Err(error));
                    return;
                }
            };
            next(Err(error));
        });

        (resolve, reject, Self { inner: Some(inner) })
    }

    pub fn then<U>(self, t: Then<T, U>) -> Promise<U, E>
    where
        U: 'static,
    {
        let (child_resolve, child_reject, child_p) = Promise::<U, E>::pending();
        let continuation: Continuation<T, E> = Box::new(move |result: Result<T, E>| match result {
            Ok(value) => {
                let new_value = t(value);
                child_resolve(new_value);
            }
            Err(error) => {
                child_reject(error);
            }
        });
        let parent_inner = self.inner.expect("Promise inner should not be None");
        let result = {
            let mut parent = parent_inner.borrow_mut();
            if !parent.resolved {
                parent.next = Some(continuation);
                return child_p;
            }
            parent.result.take()
        };
        continuation(result.expect("Promise result should not be None"));
        child_p
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_promise() {
        let promise = Promise::<i32, String>::new(|resolve, reject| {
            // Simulate an asynchronous operation
            let result = 42; // This could be the result of some computation
            resolve(result);
        });
        let p = promise
            .then(Box::new(|value| value + 1))
            .then(Box::new(|value| value + 1));

        let inner = p.inner.as_ref().unwrap().borrow();
        assert_eq!(inner.resolved, true);
        assert_eq!(inner.result, Some(Ok(44)));
    }
}
