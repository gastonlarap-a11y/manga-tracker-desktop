export namespace browsers {
	
	export class Browser {
	    id: string;
	    name: string;
	    path: string;
	
	    static createFrom(source: any = {}) {
	        return new Browser(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.path = source["path"];
	    }
	}

}

export namespace installer {
	
	export class State {
	    kind: string;
	    baseUrl: string;
	    version: string;
	
	    static createFrom(source: any = {}) {
	        return new State(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.baseUrl = source["baseUrl"];
	        this.version = source["version"];
	    }
	}

}

export namespace main {
	
	export class InstallOutcome {
	    baseUrl: string;
	    port: number;
	    dataDir: string;
	    userCanControlIt: boolean;
	    refused: string;
	
	    static createFrom(source: any = {}) {
	        return new InstallOutcome(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.baseUrl = source["baseUrl"];
	        this.port = source["port"];
	        this.dataDir = source["dataDir"];
	        this.userCanControlIt = source["userCanControlIt"];
	        this.refused = source["refused"];
	    }
	}
	export class Settings {
	    hasPayload: boolean;
	    installed: boolean;
	    port: number;
	    dataDir: string;
	    extensionDir: string;
	    version: string;
	    syncConfigured: boolean;
	    browsers: browsers.Browser[];
	    storeUrl: string;
	    problem: string;
	
	    static createFrom(source: any = {}) {
	        return new Settings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.hasPayload = source["hasPayload"];
	        this.installed = source["installed"];
	        this.port = source["port"];
	        this.dataDir = source["dataDir"];
	        this.extensionDir = source["extensionDir"];
	        this.version = source["version"];
	        this.syncConfigured = source["syncConfigured"];
	        this.browsers = this.convertValues(source["browsers"], browsers.Browser);
	        this.storeUrl = source["storeUrl"];
	        this.problem = source["problem"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class SyncOutcome {
	    problem: string;
	    connected: boolean;
	    lastError: string;
	
	    static createFrom(source: any = {}) {
	        return new SyncOutcome(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.problem = source["problem"];
	        this.connected = source["connected"];
	        this.lastError = source["lastError"];
	    }
	}

}

