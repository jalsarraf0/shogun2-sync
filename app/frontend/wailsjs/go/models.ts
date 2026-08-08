export namespace config {
	
	export class Config {
	    cloud_provider: string;
	    cloud_root: string;
	    sync_subfolder: string;
	    save_path?: string;
	
	    static createFrom(source: any = {}) {
	        return new Config(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.cloud_provider = source["cloud_provider"];
	        this.cloud_root = source["cloud_root"];
	        this.sync_subfolder = source["sync_subfolder"];
	        this.save_path = source["save_path"];
	    }
	}

}

export namespace conflicts {
	
	export class File {
	    path: string;
	    name: string;
	    modified: number;
	
	    static createFrom(source: any = {}) {
	        return new File(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.name = source["name"];
	        this.modified = source["modified"];
	    }
	}

}

export namespace main {
	
	export class GoogleDriveAuthResult {
	    ok: boolean;
	    error?: string;
	    account?: string;
	
	    static createFrom(source: any = {}) {
	        return new GoogleDriveAuthResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.error = source["error"];
	        this.account = source["account"];
	    }
	}
	export class GoogleDriveMirrorStatus {
	    applicable: boolean;
	    enabled: boolean;
	    active: boolean;
	    lastSync?: string;
	
	    static createFrom(source: any = {}) {
	        return new GoogleDriveMirrorStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.applicable = source["applicable"];
	        this.enabled = source["enabled"];
	        this.active = source["active"];
	        this.lastSync = source["lastSync"];
	    }
	}

}

export namespace orchestrate {
	
	export class RecoverResult {
	    ok: boolean;
	    error?: string;
	    conflicts: conflicts.File[];
	    recent?: conflicts.File[];
	
	    static createFrom(source: any = {}) {
	        return new RecoverResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.error = source["error"];
	        this.conflicts = this.convertValues(source["conflicts"], conflicts.File);
	        this.recent = this.convertValues(source["recent"], conflicts.File);
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
	export class SetupResult {
	    ok: boolean;
	    error?: string;
	    alreadySet: boolean;
	    savePath: string;
	    syncTarget: string;
	
	    static createFrom(source: any = {}) {
	        return new SetupResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.error = source["error"];
	        this.alreadySet = source["alreadySet"];
	        this.savePath = source["savePath"];
	        this.syncTarget = source["syncTarget"];
	    }
	}
	export class StatusResult {
	    configExists: boolean;
	    savePath: string;
	    syncTarget: string;
	    linked: boolean;
	    linkedOk: boolean;
	
	    static createFrom(source: any = {}) {
	        return new StatusResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.configExists = source["configExists"];
	        this.savePath = source["savePath"];
	        this.syncTarget = source["syncTarget"];
	        this.linked = source["linked"];
	        this.linkedOk = source["linkedOk"];
	    }
	}

}

