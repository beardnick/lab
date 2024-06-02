const http = require('http');
const https = require('https');
const http2 = require('http2');
const fs = require('fs');
const url = require('url');
const { promisify } = require('util');
const net = require('net');

/**
 * @typedef Options
 * @property {boolean} https
 * @property {boolean} http2
 */

/**
 * 
 * @param {Options} opt 
 * @returns 
 */
function createServer(opt) {
	let server;
	if (opt.http2) {
		server = http2.createSecureServer(
			{
				key: fs.readFileSync('server.key'),
				cert: fs.readFileSync('server.crt'),
			},
		);
	} else {
		if (opt.https) {
			server = https.createServer(
				{
					key: fs.readFileSync('server.key'),
					cert: fs.readFileSync('server.crt'),
				},
			);
		} else {
			server = http.createServer();
		}
	}
	server.promise = {
		listen: promisify(server.listen.bind(server)),
		close: promisify(server.close.bind(server)),
	}
	return server;
}

/**
 * @param {http.IncomingMessage} req
 * @param {http.ServerResponse<http.IncomingMessage>} res
 */
function onRequest(req, res) {
	res.statusCode = 200;
	res.end('hello world');
}

function onUpgrade(req, socket, head) {
	socket.write('HTTP/1.1 101 Web Socket Protocol Handshake\r\n' +
		'Upgrade: WebSocket\r\n' +
		'Connection: Upgrade\r\n' +
		'\r\n');

	socket.pipe(socket); // echo back
}

async function request(options) {
	// const options = {
	//   hostname: 'api.example.com',
	//   path: '/endpoint',
	//   method: 'GET'
	// };


	return new Promise((resolve, reject) => {
		const onResponse = (res) => {
			let data = '';

			res.on('data', (chunk) => {
				data += chunk;
			});

			res.on('end', () => {
				resolve(data);
			});
		};
		let req;
		if (options.http2) {
			const u = url.parse(options.url);
			const client = http2.connect(options.url, options);
			req = client.request(
				{
					':path': u.path,
				},
			);
			req.setEncoding('utf8');
			let data = '';
			req.on('data', (chunk) => { data += chunk; });
			req.on('end', () => {
				client.close();
				resolve(data);
			});
			req.end();
			return;
		}
		if (options.url.startsWith('https')) {
			req = https.request(options.url, options, onResponse);
		} else {
			req = http.request(options.url, options, onResponse);
		}
		req.on('error', (error) => {
			reject(error);
		});

		req.on('upgrade', (res, socket, upgradeHead) => {
			socket.end();
			resolve(null);
		});

		if (options.data) {
			if (typeof options.data === 'string') {
				req.write(options.data);
			} else {
				req.write(JSON.stringify(options.data));
			}
		}
		req.end();
	});
}

function getFreePort() {
	return new Promise((resolve, reject) => {
		const server = net.createServer();
		server.unref();
		server.on('error', reject);
		server.listen(0, () => {
			const { port } = server.address();
			server.close(() => {
				resolve(port);
			});
		});
	});
}

module.exports = {
	createServer,
	onRequest,
	onUpgrade,
	request,
	getFreePort,
};