var myTree;
var res;

async function showContent() {
    let x = document.getElementById("frm1");
    document.getElementById("content").innerHTML = 'loading...';
    document.getElementById("saver").innerHTML = '';
    document.getElementById("instructions").innerHTML = '';
    document.getElementById("legend").innerHTML = '';
    let data = {
        ghToken: x["token"].value,
        ghUser: x["owner"].value,
        repo: x["repo"].value,
        hash: x["ref"].value,
        persistentId: x["persistentId"].value,
        dataverseKey: x["apiKey"].value,
    };
    let fetched = await fetch("../../api/github/tree", {
        method: "POST",
        body: JSON.stringify(data),
    });
    if (fetched.status != 200) {
        alert(await fetched.text());
        document.getElementById("content").innerHTML = '';
        return;
    }
    res = await fetched.json();
    myTree = new Tree('#content', {
        data: res.children,
    });
    if (res.children.length > 0) {
        document.getElementById("saver").innerHTML = '<button onclick="confirm()">Save</button>';
        document.getElementById("instructions").innerHTML = '<span style="font-weight: bold;">Select the files to <span style="font-weight: 900; color: red;">KEEP</span> in Dataverse (regardless of the color):<br/></span><button onclick="myTree.collapseAll()">Collaps all</button>';
        document.getElementById("legend").innerHTML = `
        Legend:
            <ul>
                <li><span style="color: green;">Files only in Github</span></li>
                <li><span style="color: black;">The same version in Github as in Dataverse</span></li>
                <li><span style="color: blue;">Github version does not match Dataverse version</span></li>
                <li><span style="color: gray;">Files only in Dataverse</span></li>
            </ul>
        `;
    }
}

async function store() {
    let x = document.getElementById("frm1");
    let data = {
        ghToken: x["token"].value,
        ghUser: x["owner"].value,
        repo: x["repo"].value,
        persistentId: x["persistentId"].value,
        dataverseKey: x["apiKey"].value,
        selectedNodes: myTree.selectedNodes,
        originalRoot: res,
        toUpdate: getSelected('toUpdate'),
        toDelete: getSelected('toDelete'),
        toAdd: getSelected('toAdd'),
    };
    let fetched = await fetch("../../api/github/store", {
        method: "POST",
        body: JSON.stringify(data),
    })
    if (fetched.status != 200) {
        alert(await fetched.text());
        document.getElementById("content").innerHTML = '';
    } else {
        showContent();
    }
}

function getSelected(fName) {
    var values = [];
    var cbs = document.forms[fName].elements['files'];
    if (!cbs) {
        return values;
    }
    if (cbs.checked) { //if only one checkbox in the form
        values.push(cbs.value);
        return values;
    }
    for (var i = 0, cbLen = cbs.length; i < cbLen; i++) {
        if (cbs[i].checked) {
            values.push(cbs[i].value);
        }
    }
    return values;
}

async function confirm() {
    let x = document.getElementById("frm1");
    let data = {
        ghToken: x["token"].value,
        ghUser: x["owner"].value,
        repo: x["repo"].value,
        persistentId: x["persistentId"].value,
        dataverseKey: x["apiKey"].value,
        selectedNodes: myTree.selectedNodes,
        originalRoot: res,
    };
    let fetched = await fetch("../../api/github/writable", {
        method: "POST",
        body: JSON.stringify(data),
    })
    if (fetched.status != 200) {
        alert(await fetched.text());
        document.getElementById("content").innerHTML = '';
    } else {
        let toConfirm = await fetched.json()
        showConfirmationDialog(toConfirm);
    }
}

function showConfirmationDialog(toConfirm) {
    document.getElementById("saver").innerHTML = '';
    document.getElementById("instructions").innerHTML = '';
    document.getElementById("legend").innerHTML = '';

    if (toConfirm.toUpdate.length == 0 && toConfirm.toDelete.length == 0 && toConfirm.toAdd.length == 0) {
        document.getElementById("content").innerHTML = 'Nothing to update, add or to delete...';
        return;
    }

    let form = '';
    if (toConfirm.toDelete.length != 0) {
        form += '<span style="font-weight: bold;">Files that will be <span style="font-weight: 900; color: red;">DELETED</span> from Dataverse:</span><form name="toDelete">';
        for (let i = 0, l = toConfirm.toDelete.length; i < l; i++) {
            form += toCheckbox(toConfirm.toDelete[i], 'red');
        }
        form += '</form>';
    } else {
        form += '<form name="toDelete"></form>';
    }

    if (toConfirm.toAdd.length != 0) {
        form += '<span style="font-weight: bold;">Files that will be <span style="font-weight: 900; color: green;">ADDED</span> to Dataverse:</span><form name="toAdd">';
        for (let i = 0, l = toConfirm.toAdd.length; i < l; i++) {
            form += toCheckbox(toConfirm.toAdd[i], 'green');
        }
        form += '</form>';
    } else {
        form += '<form name="toAdd"></form>';
    }

    if (toConfirm.toUpdate.length != 0) {
        form += '<span style="font-weight: bold;">Files that will be <span style="font-weight: 900; color: blue;">UPDATED</span> in Dataverse:</span><form name="toUpdate">';
        for (let i = 0, l = toConfirm.toUpdate.length; i < l; i++) {
            form += toCheckbox(toConfirm.toUpdate[i], 'blue');
        }
        form += '</form>';
    } else {
        form += '<form name="toUpdate"></form>';
    }
    
    form += '<span><br/></span><button onclick="store()">OK</button><button onclick="showContent()">Cancel</button>';

    document.getElementById("content").innerHTML = form;
}

function toCheckbox(value, color) {
    return '<p><input type="checkbox" name="files" value="' + value + '" checked="true"/><span style="color: ' + color + ';">' + value + '</span></p>';
}