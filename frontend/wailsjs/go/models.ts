export namespace installer {
	
	export class Result {
	    baseUrl: string;
	    port: number;
	    dataDir: string;
	    userCanControlIt: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Result(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.baseUrl = source["baseUrl"];
	        this.port = source["port"];
	        this.dataDir = source["dataDir"];
	        this.userCanControlIt = source["userCanControlIt"];
	    }
	}
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

