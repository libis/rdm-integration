var res;

async function showContent() {
    let x = document.getElementById("frm1");
    document.getElementById("content").innerHTML = 'loading...';
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
        onChange: function() {
          showConfirmationDialog();
        },
    });
    if (res.children.length > 0) {
        document.getElementById("instructions").innerHTML = '<span style="font-weight: bold;">Select the files to <span style="font-weight: 900; color: red;">KEEP</span> in Dataverse:<br/><br/></span><button onclick="myTree.collapseAll()">Collaps all</button>';
        document.getElementById("legend").innerHTML = `
        Legend:
            <ul>
                <li><span style="color: green;">Files only in Github</span></li>
                <li><span style="color: black;">The same version in Github as in Dataverse</span></li>
                <li><span style="color: blue;">Github version does not match Dataverse version</span></li>
                <li><span style="color: gray;">Files only in Dataverse</span></li>
            </ul>
        `;
        x.style.display = 'none';
        showConfirmationDialog();
    } else {
        document.getElementById("content").innerHTML = 'No files found.';
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
    } else {
        cancel();
    }
}
