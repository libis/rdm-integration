var res;

async function showContent() {
    let x = document.getElementById("frm1");
    document.getElementById("content").innerHTML = 'loading...';
    let data = {
        ghToken: x["ghToken"].value,
        ghUser: x["owner"].value,
        repo: x["repo"].value,
        hash: x["ref"].value,
        persistentId: x["persistentId"].value,
        dataverseKey: x["dataverseKey"].value,
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
        onChange: function () {
            showConfirmationDialog();
        },
    });
    localStorage.setItem("dataverseKey", x["dataverseKey"].value);
    localStorage.setItem("ghToken", x["ghToken"].value);
    if (res.children.length > 0) {
        showConfirmationDialog();
    } else {
        document.getElementById("content").innerHTML = 'No files found.';
    }
}

async function store() {
    let x = document.getElementById("frm1");
    let data = {
        ghToken: x["ghToken"].value,
        ghUser: x["owner"].value,
        repo: x["repo"].value,
        persistentId: x["persistentId"].value,
        dataverseKey: x["dataverseKey"].value,
        selectedNodes: myTree.selectedNodes,
        originalRoot: res,
        toUpdate: getValues('toUpdate', true),
        toDelete: getValues('toDelete', true),
        toAdd: getValues('toAdd', true),
    };
    let fetched = await fetch("../../api/github/store", {
        method: "POST",
        body: JSON.stringify(data),
    });
    if (fetched.status != 200) {
        alert(await fetched.text());
    } else {
        cancel();
    }
}
