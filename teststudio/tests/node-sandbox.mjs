import {parentPort} from 'node:worker_threads';
globalThis.self = globalThis;
self.postMessage = data => parentPort.postMessage(data);
await import('../dist/sandbox.worker.js');
parentPort.on('message', data => self.onmessage({data}));
