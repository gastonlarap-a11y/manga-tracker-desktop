export namespace main {
	
	export class BackendStatus {
	    found: boolean;
	    baseUrl: string;
	    firstPort: number;
	    lastPort: number;
	
	    static createFrom(source: any = {}) {
	        return new BackendStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.found = source["found"];
	        this.baseUrl = source["baseUrl"];
	        this.firstPort = source["firstPort"];
	        this.lastPort = source["lastPort"];
	    }
	}

}

